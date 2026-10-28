// Package apk reads what ghapk needs to know about an APK without aapt,
// apksigner, unzip or openssl: the package name, versionCode and versionName
// out of the binary AndroidManifest.xml, and the SHA-256 of the signing
// certificate out of the v2/v3 signing block or the v1 META-INF signature.
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
	"strings"
	"time"
	"unicode/utf16"
)

// Info is what the manifest says about the app.
type Info struct {
	Package     string
	VersionCode int64
	VersionName string
	MinSDK      int
	TargetSDK   int
	Debuggable  bool
}

// ReadInfo parses AndroidManifest.xml inside the APK.
func ReadInfo(path string) (Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Info{}, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "AndroidManifest.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Info{}, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 32<<20))
		rc.Close()
		if err != nil {
			return Info{}, err
		}
		return parseManifest(b)
	}
	return Info{}, errors.New("no AndroidManifest.xml (not an APK)")
}

type attr struct {
	elem, name, val string
	num             int64
	typ             byte
}

func parseManifest(b []byte) (Info, error) {
	attrs, err := parseAXML(b)
	if err != nil {
		return Info{}, err
	}
	var in Info
	for _, a := range attrs {
		switch {
		case a.elem == "manifest" && a.name == "package" && in.Package == "":
			in.Package = a.val
		case a.elem == "manifest" && a.name == "versionCode" && in.VersionCode == 0:
			in.VersionCode = a.num
		case a.elem == "manifest" && a.name == "versionName" && in.VersionName == "":
			if a.typ == 0x03 {
				in.VersionName = a.val
			}
		case a.elem == "uses-sdk" && a.name == "minSdkVersion":
			in.MinSDK = int(a.num)
		case a.elem == "uses-sdk" && a.name == "targetSdkVersion":
			in.TargetSDK = int(a.num)
		case a.elem == "application" && a.name == "debuggable":
			in.Debuggable = a.num != 0
		}
	}
	if in.Package == "" {
		return in, errors.New("manifest has no package attribute")
	}
	return in, nil
}

func u16(b []byte, o int) int { return int(binary.LittleEndian.Uint16(b[o:])) }
func u32(b []byte, o int) int { return int(binary.LittleEndian.Uint32(b[o:])) }

// parseAXML walks the binary XML chunks and returns every attribute of every
// start element.
func parseAXML(b []byte) ([]attr, error) {
	if len(b) < 8 || u16(b, 0) != 0x0003 {
		return nil, errors.New("not a binary AndroidManifest.xml")
	}
	var pool []string
	var out []attr
	str := func(i int) string {
		if i >= 0 && i < len(pool) {
			return pool[i]
		}
		return ""
	}
	for off := 8; off+8 <= len(b); {
		typ, hsz, sz := u16(b, off), u16(b, off+2), u32(b, off+4)
		if sz < 8 || off+sz > len(b) {
			break
		}
		c := b[off : off+sz]
		switch typ {
		case 0x0001: // string pool
			p, err := parsePool(c)
			if err != nil {
				return nil, err
			}
			pool = p
		case 0x0102: // start element
			if hsz+20 > len(c) {
				break
			}
			ext := hsz
			name := str(u32(c, ext+4))
			aStart, aSize, aCount := u16(c, ext+8), u16(c, ext+10), u16(c, ext+12)
			if aSize < 20 {
				aSize = 20
			}
			for i := 0; i < aCount; i++ {
				a := ext + aStart + i*aSize
				if a+20 > len(c) {
					break
				}
				at := attr{elem: name, name: str(u32(c, a+4)), typ: c[a+15]}
				data := int64(uint32(u32(c, a+16)))
				switch at.typ {
				case 0x03:
					at.val = str(int(data))
					if raw := u32(c, a+8); at.val == "" && raw != -1 && raw != 0xFFFFFFFF {
						at.val = str(raw)
					}
				case 0x10, 0x11, 0x12:
					at.num = data
					at.val = fmt.Sprint(data)
				default:
					at.num = data
					at.val = fmt.Sprintf("0x%x", data)
				}
				out = append(out, at)
			}
		}
		off += sz
	}
	return out, nil
}

func parsePool(c []byte) ([]string, error) {
	if len(c) < 28 {
		return nil, errors.New("short string pool")
	}
	n, flags, sStart := u32(c, 8), u32(c, 16), u32(c, 20)
	utf8 := flags&0x100 != 0
	hsz := u16(c, 2)
	if n < 0 || n > 1<<20 || hsz+4*n > len(c) {
		return nil, errors.New("bad string pool")
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		o := sStart + u32(c, hsz+4*i)
		if o < 0 || o >= len(c) {
			continue
		}
		if utf8 {
			_, l1 := lenUTF8(c, o)
			bl, l2 := lenUTF8(c, o+l1)
			s := o + l1 + l2
			if s+bl <= len(c) {
				out[i] = string(c[s : s+bl])
			}
		} else {
			l := u16(c, o)
			s := o + 2
			if l&0x8000 != 0 {
				l = (l&0x7fff)<<16 | u16(c, o+2)
				s = o + 4
			}
			if s+2*l <= len(c) {
				w := make([]uint16, l)
				for k := range w {
					w[k] = uint16(u16(c, s+2*k))
				}
				out[i] = string(utf16.Decode(w))
			}
		}
	}
	return out, nil
}

// lenUTF8 decodes the 1-or-2 byte length prefix used by UTF-8 string pools.
func lenUTF8(c []byte, o int) (n, used int) {
	if o >= len(c) {
		return 0, 1
	}
	if c[o]&0x80 != 0 && o+1 < len(c) {
		return int(c[o]&0x7f)<<8 | int(c[o+1]), 2
	}
	return int(c[o]), 1
}

// ---- hashing ----------------------------------------------------------------

// FileSHA256 returns the hex SHA-256 of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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

// ---- signing certificate ----------------------------------------------------

// Cert identifies the key an APK is signed with.
type Cert struct {
	SHA256   string // hex of the DER certificate; the fingerprint Android shows
	Subject  string
	NotAfter time.Time
	Scheme   string // "v3", "v2" or "v1"
}

// ReadCert finds the signing certificate: the v3/v2 signing block first, then
// the v1 META-INF signature file.
func ReadCert(path string) (Cert, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cert{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Cert{}, err
	}
	if der, scheme := blockCert(f, st.Size()); der != nil {
		return mkCert(der, scheme), nil
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return Cert{}, err
	}
	for _, zf := range zr.File {
		n := strings.ToUpper(zf.Name)
		if !strings.HasPrefix(n, "META-INF/") || !(strings.HasSuffix(n, ".RSA") || strings.HasSuffix(n, ".DSA") || strings.HasSuffix(n, ".EC")) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if der := pkcs7Cert(b); der != nil {
			return mkCert(der, "v1"), nil
		}
	}
	return Cert{}, errors.New("no readable signature")
}

func mkCert(der []byte, scheme string) Cert {
	sum := sha256.Sum256(der)
	c := Cert{SHA256: hex.EncodeToString(sum[:]), Scheme: scheme}
	if x, err := x509.ParseCertificate(der); err == nil {
		c.Subject = x.Subject.String()
		c.NotAfter = x.NotAfter
	}
	return c
}

// Fingerprint is the SHA-256 of a DER certificate given as hex (what
// `dumpsys package` prints), or "" if the hex is not valid.
func Fingerprint(hexDER string) string {
	der, err := hex.DecodeString(strings.TrimSpace(hexDER))
	if err != nil || len(der) == 0 {
		return ""
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

const (
	idV2  = 0x7109871a
	idV3  = 0xf05368c0
	idV31 = 0x1b93ad61
)

// blockCert reads the first signer certificate of an APK Signature Scheme
// v2/v3 block. The block sits just before the zip central directory.
func blockCert(f *os.File, size int64) ([]byte, string) {
	tail := int64(66000)
	if tail > size {
		tail = size
	}
	buf := make([]byte, tail)
	if _, err := f.ReadAt(buf, size-tail); err != nil && err != io.EOF {
		return nil, ""
	}
	e := -1
	for i := len(buf) - 22; i >= 0; i-- {
		if buf[i] == 'P' && buf[i+1] == 'K' && buf[i+2] == 5 && buf[i+3] == 6 {
			e = i
			break
		}
	}
	if e < 0 {
		return nil, ""
	}
	cdOff := int64(binary.LittleEndian.Uint32(buf[e+16:]))
	if cdOff < 32 || cdOff > size {
		return nil, ""
	}
	foot := make([]byte, 24)
	if _, err := f.ReadAt(foot, cdOff-24); err != nil || string(foot[8:]) != "APK Sig Block 42" {
		return nil, ""
	}
	bsz := int64(binary.LittleEndian.Uint64(foot[:8]))
	if bsz < 24 || bsz > cdOff || bsz > 64<<20 {
		return nil, ""
	}
	// bsz counts everything after the leading 8-byte size field: the pairs,
	// then a copy of the size (8) and the magic (16).
	blk := make([]byte, bsz)
	if _, err := f.ReadAt(blk, cdOff-bsz); err != nil {
		return nil, ""
	}
	pairs := blk[:len(blk)-24]
	var v2, v3 []byte
	for len(pairs) >= 12 {
		l := binary.LittleEndian.Uint64(pairs)
		if l < 4 || int64(l) > int64(len(pairs)-8) {
			break
		}
		id := binary.LittleEndian.Uint32(pairs[8:])
		val := pairs[12 : 8+int(l)]
		switch id {
		case idV3, idV31:
			if v3 == nil {
				v3 = val
			}
		case idV2:
			v2 = val
		}
		pairs = pairs[8+int(l):]
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

// lp reads one u32-length-prefixed field.
func lp(b []byte) (field, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	if n < 0 || 4+n > len(b) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// firstSignerCert digs the first certificate out of a v2/v3 signers sequence:
// signers > signer > signed data > certificates > certificate.
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

// pkcs7Cert returns the first certificate in a DER PKCS#7 SignedData blob.
func pkcs7Cert(b []byte) []byte {
	var top asn1.RawValue
	if _, err := asn1.Unmarshal(b, &top); err != nil || !top.IsCompound {
		return nil
	}
	kids := children(top.Bytes)
	if len(kids) < 2 {
		return nil
	}
	sdWrap := children(kids[1].Bytes)
	if len(sdWrap) == 0 {
		return nil
	}
	for _, k := range children(sdWrap[0].Bytes) {
		if k.Class == asn1.ClassContextSpecific && k.Tag == 0 {
			if cs := children(k.Bytes); len(cs) > 0 {
				return cs[0].FullBytes
			}
		}
	}
	return nil
}

func children(b []byte) []asn1.RawValue {
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
