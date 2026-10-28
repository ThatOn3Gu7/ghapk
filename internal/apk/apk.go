// Package apk reads Android APK metadata, signing certificates, and download
// digests in pure Go without external Android SDK binaries (aapt2/apksigner).
package apk

import (
	"archive/zip"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf16"
)

const (
	maxManifestBytes = 8 << 20  // 8 MiB bound on AndroidManifest.xml
	maxCertBlobBytes = 8 << 20  // 8 MiB bound on META-INF/*.RSA|DSA|EC
	maxSigBlockBytes = 16 << 20 // 16 MiB bound on APK Signing Block
)

// Info is the identity metadata declared in AndroidManifest.xml.
type Info struct {
	Package     string
	VersionCode int64
	VersionName string
	MinSDK      int
	TargetSDK   int
}

// Cert describes the APK's first active signing certificate.
type Cert struct {
	SHA256  string // lowercase 64-char hex SHA-256 of DER X.509 certificate
	Subject string // CommonName or formatted Subject when parseable
	Scheme  string // "v3", "v2", or "v1"
}

// DigestStatus reports whether a file's SHA-256 was checked against an
// independent trusted digest published by GitHub.
type DigestStatus string

const (
	// DigestVerified means an expected sha256 digest was published by a
	// trusted metadata source (e.g., GitHub API) and matched the file.
	DigestVerified DigestStatus = "verified"

	// DigestUnverified means no expected digest was available to compare
	// against. A self-computed SHA-256 of an untrusted download is NEVER
	// treated as DigestVerified.
	DigestUnverified DigestStatus = "unverified"

	// DigestMismatch means an expected digest was provided and did not match.
	DigestMismatch DigestStatus = "mismatch"
)

// Inspection combines manifest metadata, signing certificate, and local hash.
type Inspection struct {
	Info         Info
	HaveInfo     bool
	InfoErr      string
	Cert         Cert
	Size         int64
	LocalSHA256  string       // informational hash of the APK file itself
	DigestStatus DigestStatus // verified ONLY when checked against trusted expected hash
}

// Inspect reads manifest info, signing certificate, size, and local SHA-256
// for path. A non-fatal manifest parse failure leaves HaveInfo=false and
// populates InfoErr so callers can warn honestly without crashing.
func Inspect(apkPath string) (Inspection, error) {
	st, err := os.Lstat(apkPath)
	if err != nil {
		return Inspection{}, err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return Inspection{}, errors.New("APK is empty or not a regular file")
	}
	sha, err := FileSHA256(apkPath)
	if err != nil {
		return Inspection{}, err
	}
	out := Inspection{
		Size:         st.Size(),
		LocalSHA256:  sha,
		DigestStatus: DigestUnverified,
	}
	if info, ierr := ReadInfo(apkPath); ierr == nil {
		out.Info = info
		out.HaveInfo = true
	} else {
		out.InfoErr = ierr.Error()
	}
	if cert, cerr := ReadCert(apkPath); cerr == nil {
		out.Cert = cert
	}
	return out, nil
}

// FileSHA256 returns the lowercase hex SHA-256 of a regular file.
func FileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// NormalizeExpectedSHA256 parses an expected digest string such as
// "sha256:0123..." or a bare 64-character hex SHA-256. Empty/"null" returns
// ("", false, nil) indicating no published digest exists. Unsupported or
// malformed non-empty digests return an error so callers never silently skip
// a broken checksum specification.
func NormalizeExpectedSHA256(expected string) (hexDigest string, present bool, err error) {
	s := strings.TrimSpace(expected)
	if s == "" || strings.EqualFold(s, "null") {
		return "", false, nil
	}
	if strings.Contains(s, ":") {
		alg, rest, _ := strings.Cut(s, ":")
		if !strings.EqualFold(alg, "sha256") {
			return "", true, fmt.Errorf("unsupported digest algorithm %q (expected sha256)", alg)
		}
		s = strings.TrimSpace(rest)
	}
	if len(s) != 64 {
		return "", true, fmt.Errorf("malformed expected sha256 digest (got %d hex chars, want 64)", len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "", true, errors.New("malformed expected sha256 digest: non-hex character")
		}
	}
	return strings.ToLower(s), true, nil
}

// VerifyExpectedSHA256 compares the SHA-256 of path against a trusted expected
// digest. When expected is empty/"null", it returns DigestUnverified (never
// DigestVerified) along with the file's local SHA-256. When expected is set
// and does not match, it returns DigestMismatch and a non-nil error.
func VerifyExpectedSHA256(filePath, expected string) (status DigestStatus, actual string, err error) {
	want, present, perr := NormalizeExpectedSHA256(expected)
	if perr != nil {
		return DigestMismatch, "", perr
	}
	got, herr := FileSHA256(filePath)
	if herr != nil {
		return DigestUnverified, "", herr
	}
	if !present {
		return DigestUnverified, got, nil
	}
	if strings.EqualFold(got, want) {
		return DigestVerified, got, nil
	}
	return DigestMismatch, got, fmt.Errorf(
		"SHA-256 mismatch against published digest (expected %.12s…, got %.12s…)",
		want, got,
	)
}

// ReadInfo opens an APK and parses its binary AndroidManifest.xml.
func ReadInfo(apkPath string) (Info, error) {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return Info{}, fmt.Errorf("not a valid zip/APK: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "AndroidManifest.xml" {
			if f.UncompressedSize64 > maxManifestBytes {
				return Info{}, errors.New("AndroidManifest.xml exceeds maximum safe size")
			}
			rc, err := f.Open()
			if err != nil {
				return Info{}, err
			}
			b, rerr := io.ReadAll(io.LimitReader(rc, maxManifestBytes+1))
			_ = rc.Close()
			if rerr != nil {
				return Info{}, rerr
			}
			if len(b) > maxManifestBytes {
				return Info{}, errors.New("AndroidManifest.xml exceeds maximum safe size")
			}
			return parseAXML(b)
		}
	}
	return Info{}, errors.New("APK has no AndroidManifest.xml")
}

// ---- binary XML (AXML) ------------------------------------------------------

const (
	chunkXML        = 0x0003
	chunkStringPool = 0x0001
	chunkResourceID = 0x0180
	chunkStartElem  = 0x0102

	flagUTF8 = 1 << 8

	typeString = 0x03
	typeIntDec = 0x10
	typeIntHex = 0x11

	resVersionCode = 0x0101021b
	resVersionName = 0x0101021c
	resMinSDK      = 0x0101020c
	resTargetSDK   = 0x01010270
)

func parseAXML(b []byte) (Info, error) {
	if len(b) < 8 || binary.LittleEndian.Uint16(b) != chunkXML {
		return Info{}, errors.New("AndroidManifest.xml is not binary AXML")
	}
	hdr := int(binary.LittleEndian.Uint16(b[2:]))
	if hdr < 8 || hdr > len(b) {
		return Info{}, errors.New("corrupt AXML header")
	}
	var (
		strs   []string
		resIDs []uint32
		out    Info
	)
	for pos := hdr; pos+8 <= len(b); {
		typ := binary.LittleEndian.Uint16(b[pos:])
		hsz := int(binary.LittleEndian.Uint16(b[pos+2:]))
		csz64 := uint64(binary.LittleEndian.Uint32(b[pos+4:]))
		if csz64 < 8 || csz64 > uint64(len(b)-pos) {
			break
		}
		csz := int(csz64)
		chunk := b[pos : pos+csz]
		pos += csz
		switch typ {
		case chunkStringPool:
			strs = readStrings(chunk)
		case chunkResourceID:
			if hsz >= 8 && hsz <= len(chunk) {
				n := (len(chunk) - hsz) / 4
				resIDs = make([]uint32, n)
				for i := 0; i < n; i++ {
					resIDs[i] = binary.LittleEndian.Uint32(chunk[hsz+i*4:])
				}
			}
		case chunkStartElem:
			readElem(chunk, hsz, strs, resIDs, &out)
		}
	}
	if out.Package == "" {
		return out, errors.New("manifest has no package attribute")
	}
	return out, nil
}

func readStrings(chunk []byte) []string {
	if len(chunk) < 28 {
		return nil
	}
	cnt64 := uint64(binary.LittleEndian.Uint32(chunk[8:]))
	if cnt64 > 200000 {
		return nil
	}
	cnt := int(cnt64)
	flags := binary.LittleEndian.Uint32(chunk[16:])
	start64 := uint64(binary.LittleEndian.Uint32(chunk[20:]))
	hsz := int(binary.LittleEndian.Uint16(chunk[2:]))
	if hsz < 28 || hsz+cnt*4 > len(chunk) || start64 > uint64(len(chunk)) {
		return nil
	}
	start := int(start64)
	utf8 := flags&flagUTF8 != 0
	out := make([]string, cnt)
	for i := 0; i < cnt; i++ {
		off64 := uint64(binary.LittleEndian.Uint32(chunk[hsz+i*4:]))
		if uint64(start)+off64 >= uint64(len(chunk)) {
			continue
		}
		p := start + int(off64)
		if utf8 {
			_, n1 := u8len(chunk[p:])
			p += n1
			if p >= len(chunk) {
				continue
			}
			blen, n2 := u8len(chunk[p:])
			p += n2
			if blen >= 0 && p+blen <= len(chunk) {
				out[i] = string(chunk[p : p+blen])
			}
		} else {
			clen, n := u16len(chunk[p:])
			p += n
			if clen >= 0 && clen <= (len(chunk)-p)/2 {
				u := make([]uint16, clen)
				for j := 0; j < clen; j++ {
					u[j] = binary.LittleEndian.Uint16(chunk[p+j*2:])
				}
				out[i] = string(utf16.Decode(u))
			}
		}
	}
	return out
}

func u8len(b []byte) (int, int) {
	if len(b) == 0 {
		return 0, 0
	}
	if b[0]&0x80 == 0 {
		return int(b[0]), 1
	}
	if len(b) < 2 {
		return 0, 1
	}
	return int(b[0]&0x7f)<<8 | int(b[1]), 2
}

func u16len(b []byte) (int, int) {
	if len(b) < 2 {
		return 0, 0
	}
	n := binary.LittleEndian.Uint16(b)
	if n&0x8000 == 0 {
		return int(n), 2
	}
	if len(b) < 4 {
		return 0, 2
	}
	hi := int(n&0x7fff) << 16
	lo := int(binary.LittleEndian.Uint16(b[2:]))
	return hi | lo, 4
}

func strAt(strs []string, idx uint32) string {
	if uint64(idx) >= uint64(len(strs)) {
		return ""
	}
	return strs[idx]
}

func readElem(chunk []byte, hsz int, strs []string, resIDs []uint32, out *Info) {
	if hsz+20 > len(chunk) {
		return
	}
	elem := strAt(strs, binary.LittleEndian.Uint32(chunk[hsz+4:]))
	if elem != "manifest" && elem != "uses-sdk" {
		return
	}
	aStart := hsz + int(binary.LittleEndian.Uint16(chunk[hsz+8:]))
	aSize := int(binary.LittleEndian.Uint16(chunk[hsz+10:]))
	aCnt := int(binary.LittleEndian.Uint16(chunk[hsz+12:]))
	if aSize < 20 {
		return
	}
	for i := 0; i < aCnt; i++ {
		ap := aStart + i*aSize
		if ap+20 > len(chunk) {
			break
		}
		nameIdx := binary.LittleEndian.Uint32(chunk[ap+4:])
		rawVal := binary.LittleEndian.Uint32(chunk[ap+8:])
		vType := chunk[ap+15]
		vData := binary.LittleEndian.Uint32(chunk[ap+16:])
		name := strAt(strs, nameIdx)
		var rid uint32
		if uint64(nameIdx) < uint64(len(resIDs)) {
			rid = resIDs[nameIdx]
		}
		asStr := func() string {
			if vType == typeString {
				return strAt(strs, vData)
			}
			if rawVal != 0xffffffff {
				return strAt(strs, rawVal)
			}
			if vType == typeIntDec || vType == typeIntHex {
				return fmt.Sprintf("%d", int32(vData))
			}
			return ""
		}
		asInt := func() int64 {
			if vType == typeIntDec || vType == typeIntHex {
				return int64(int32(vData))
			}
			return 0
		}
		switch elem {
		case "manifest":
			switch {
			case name == "package" && rid == 0:
				out.Package = asStr()
			case rid == resVersionCode || name == "versionCode":
				out.VersionCode = asInt()
			case rid == resVersionName || name == "versionName":
				out.VersionName = asStr()
			}
		case "uses-sdk":
			switch {
			case rid == resMinSDK || name == "minSdkVersion":
				out.MinSDK = int(asInt())
			case rid == resTargetSDK || name == "targetSdkVersion":
				out.TargetSDK = int(asInt())
			}
		}
	}
}

// ---- signing certificate (APK Signature Scheme v2 / v3, or JAR v1) ----------

const (
	sigBlockMagicLo = 0x20676953204b5041 // "APK Sig "
	sigBlockMagicHi = 0x3234206b636f6c42 // "Block 42"
	idV2            = 0x7109871a
	idV3            = 0xf05368c0
	idV31           = 0x1b93ad61
)

// ReadCert extracts the first active signing certificate from an APK,
// preferring v3 -> v2 -> JAR v1 (META-INF/*.RSA|DSA|EC).
func ReadCert(apkPath string) (Cert, error) {
	f, err := os.Open(apkPath)
	if err != nil {
		return Cert{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Cert{}, err
	}
	if der, scheme := signingBlockCert(f, st.Size()); der != nil {
		return finishCert(der, scheme), nil
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return Cert{}, err
	}
	for _, zf := range zr.File {
		up := strings.ToUpper(zf.Name)
		if !strings.HasPrefix(up, "META-INF/") {
			continue
		}
		switch path.Ext(up) {
		case ".RSA", ".DSA", ".EC":
			if zf.UncompressedSize64 > maxCertBlobBytes {
				continue
			}
			rc, err := zf.Open()
			if err != nil {
				continue
			}
			b, rerr := io.ReadAll(io.LimitReader(rc, maxCertBlobBytes+1))
			_ = rc.Close()
			if rerr == nil && len(b) <= maxCertBlobBytes {
				if der := pkcs7Cert(b); der != nil {
					return finishCert(der, "v1"), nil
				}
			}
		}
	}
	return Cert{}, errors.New("no signing certificate found in APK")
}

func finishCert(der []byte, scheme string) Cert {
	sum := sha256.Sum256(der)
	c := Cert{SHA256: hex.EncodeToString(sum[:]), Scheme: scheme}
	if x, err := x509.ParseCertificate(der); err == nil {
		c.Subject = x.Subject.CommonName
		if c.Subject == "" {
			c.Subject = x.Subject.String()
		}
	}
	return c
}

func signingBlockCert(f *os.File, size int64) ([]byte, string) {
	tail := int64(65558)
	if tail > size {
		tail = size
	}
	if tail < 22 {
		return nil, ""
	}
	buf := make([]byte, tail)
	if _, err := f.ReadAt(buf, size-tail); err != nil {
		return nil, ""
	}
	eocd := -1
	for i := len(buf) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) == 0x06054b50 {
			eocd = i
			break
		}
	}
	if eocd < 0 {
		return nil, ""
	}
	cdOff := int64(binary.LittleEndian.Uint32(buf[eocd+16:]))
	if cdOff < 32 || cdOff > size {
		return nil, ""
	}
	var foot [24]byte
	if _, err := f.ReadAt(foot[:], cdOff-24); err != nil {
		return nil, ""
	}
	if binary.LittleEndian.Uint64(foot[8:]) != sigBlockMagicLo ||
		binary.LittleEndian.Uint64(foot[16:]) != sigBlockMagicHi {
		return nil, ""
	}
	bsz := binary.LittleEndian.Uint64(foot[0:])
	if bsz < 24 || bsz > maxSigBlockBytes || int64(bsz)+8 > cdOff {
		return nil, ""
	}
	total := int64(bsz) + 8
	block := make([]byte, total)
	if _, err := f.ReadAt(block, cdOff-total); err != nil {
		return nil, ""
	}
	if binary.LittleEndian.Uint64(block) != bsz {
		return nil, ""
	}
	pairs := block[8 : total-24]
	var v2, v3 []byte
	for len(pairs) >= 12 {
		l := binary.LittleEndian.Uint64(pairs)
		if l < 4 || l > uint64(len(pairs)-8) {
			break
		}
		step := int(l)
		id := binary.LittleEndian.Uint32(pairs[8:])
		val := pairs[12 : 8+step]
		switch id {
		case idV3, idV31:
			if v3 == nil {
				v3 = val
			}
		case idV2:
			v2 = val
		}
		pairs = pairs[8+step:]
	}
	if v3 != nil {
		if der := firstSignerCert(v3); der != nil {
			return der, "v3"
		}
	}
	if v2 != nil {
		if der := firstSignerCert(v2); der != nil {
			return der, "v2"
		}
	}
	return nil, ""
}

func lp(b []byte) (field, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n64 := uint64(binary.LittleEndian.Uint32(b))
	if n64 > uint64(len(b)-4) {
		return nil, nil, false
	}
	n := int(n64)
	return b[4 : 4+n], b[4+n:], true
}

func firstSignerCert(v []byte) []byte {
	signers, _, ok := lp(v)
	if !ok {
		return nil
	}
	signer, _, ok := lp(signers)
	if !ok {
		return nil
	}
	signed, _, ok := lp(signer)
	if !ok {
		return nil
	}
	_, rest, ok := lp(signed) // digests
	if !ok {
		return nil
	}
	certs, _, ok := lp(rest)
	if !ok {
		return nil
	}
	cert, _, ok := lp(certs)
	if !ok || len(cert) == 0 {
		return nil
	}
	return cert
}

func pkcs7Cert(b []byte) []byte {
	var top asn1.RawValue
	if _, err := asn1.Unmarshal(b, &top); err != nil || !top.IsCompound {
		return nil
	}
	kids := asn1Children(top.Bytes)
	if len(kids) < 2 {
		return nil
	}
	sdWrap := asn1Children(kids[1].Bytes)
	if len(sdWrap) == 0 {
		return nil
	}
	for _, k := range asn1Children(sdWrap[0].Bytes) {
		if k.Class == asn1.ClassContextSpecific && k.Tag == 0 {
			if cs := asn1Children(k.Bytes); len(cs) > 0 {
				return cs[0].FullBytes
			}
		}
	}
	return nil
}

func asn1Children(b []byte) []asn1.RawValue {
	var out []asn1.RawValue
	for len(b) > 0 {
		var rv asn1.RawValue
		rest, err := asn1.Unmarshal(b, &rv)
		if err != nil {
			break
		}
		out = append(out, rv)
		b = rest
	}
	return out
}
