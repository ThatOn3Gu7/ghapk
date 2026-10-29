package apk

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildMinimalAXML constructs a valid binary AndroidManifest.xml with UTF-8
// string pool, resource ID map, <manifest> and <uses-sdk> start elements.
func buildMinimalAXML(pkg, verName string, verCode int32, minSDK, targetSDK int32) []byte {
	stringsList := []string{
		"manifest",         // 0
		"package",          // 1
		"versionCode",      // 2
		"versionName",      // 3
		"uses-sdk",         // 4
		"minSdkVersion",    // 5
		"targetSdkVersion", // 6
		pkg,                // 7
		verName,            // 8
	}
	resIDs := []uint32{
		0,              // manifest
		0,              // package (no android: resource ID)
		resVersionCode, // versionCode
		resVersionName, // versionName
		0,              // uses-sdk
		resMinSDK,      // minSdkVersion
		resTargetSDK,   // targetSdkVersion
	}

	// String pool chunk (UTF-8)
	var strData bytes.Buffer
	offsets := make([]uint32, len(stringsList))
	for i, s := range stringsList {
		offsets[i] = uint32(strData.Len())
		b := []byte(s)
		strData.WriteByte(byte(len(b))) // char count (<128)
		strData.WriteByte(byte(len(b))) // byte count (<128)
		strData.Write(b)
		strData.WriteByte(0)
	}
	for strData.Len()%4 != 0 {
		strData.WriteByte(0)
	}

	spHeaderSize := uint16(28)
	stringsStart := uint32(spHeaderSize) + uint32(len(stringsList)*4)
	spChunkSize := stringsStart + uint32(strData.Len())

	var sp bytes.Buffer
	_ = binary.Write(&sp, binary.LittleEndian, uint16(chunkStringPool))
	_ = binary.Write(&sp, binary.LittleEndian, spHeaderSize)
	_ = binary.Write(&sp, binary.LittleEndian, spChunkSize)
	_ = binary.Write(&sp, binary.LittleEndian, uint32(len(stringsList))) // stringCount
	_ = binary.Write(&sp, binary.LittleEndian, uint32(0))                // styleCount
	_ = binary.Write(&sp, binary.LittleEndian, uint32(flagUTF8))         // flags
	_ = binary.Write(&sp, binary.LittleEndian, stringsStart)
	_ = binary.Write(&sp, binary.LittleEndian, uint32(0)) // stylesStart
	for _, off := range offsets {
		_ = binary.Write(&sp, binary.LittleEndian, off)
	}
	sp.Write(strData.Bytes())

	// Resource ID chunk
	var resBuf bytes.Buffer
	_ = binary.Write(&resBuf, binary.LittleEndian, uint16(chunkResourceID))
	_ = binary.Write(&resBuf, binary.LittleEndian, uint16(8))
	_ = binary.Write(&resBuf, binary.LittleEndian, uint32(8+len(resIDs)*4))
	for _, id := range resIDs {
		_ = binary.Write(&resBuf, binary.LittleEndian, id)
	}

	type attr struct {
		nameIdx uint32
		rawVal  uint32
		vType   uint8
		vData   uint32
	}
	makeStartElem := func(elemIdx uint32, attrs []attr) []byte {
		var eb bytes.Buffer
		hsz := uint16(16)
		csz := uint32(36 + len(attrs)*20)
		_ = binary.Write(&eb, binary.LittleEndian, uint16(chunkStartElem))
		_ = binary.Write(&eb, binary.LittleEndian, hsz)
		_ = binary.Write(&eb, binary.LittleEndian, csz)
		_ = binary.Write(&eb, binary.LittleEndian, uint32(1)) // lineNumber
		_ = binary.Write(&eb, binary.LittleEndian, uint32(0xffffffff))
		// body (20 bytes before attributes)
		_ = binary.Write(&eb, binary.LittleEndian, uint32(0xffffffff)) // ns
		_ = binary.Write(&eb, binary.LittleEndian, elemIdx)            // name
		_ = binary.Write(&eb, binary.LittleEndian, uint16(20))         // attrStart
		_ = binary.Write(&eb, binary.LittleEndian, uint16(20))         // attrSize
		_ = binary.Write(&eb, binary.LittleEndian, uint16(len(attrs))) // attrCount
		_ = binary.Write(&eb, binary.LittleEndian, uint16(0))
		_ = binary.Write(&eb, binary.LittleEndian, uint16(0))
		_ = binary.Write(&eb, binary.LittleEndian, uint16(0))
		for _, a := range attrs {
			_ = binary.Write(&eb, binary.LittleEndian, uint32(0xffffffff)) // ns
			_ = binary.Write(&eb, binary.LittleEndian, a.nameIdx)
			_ = binary.Write(&eb, binary.LittleEndian, a.rawVal)
			_ = binary.Write(&eb, binary.LittleEndian, uint16(8)) // size
			eb.WriteByte(0)
			eb.WriteByte(a.vType)
			_ = binary.Write(&eb, binary.LittleEndian, a.vData)
		}
		return eb.Bytes()
	}

	manifestElem := makeStartElem(0, []attr{
		{nameIdx: 1, rawVal: 7, vType: typeString, vData: 7},
		{nameIdx: 2, rawVal: 0xffffffff, vType: typeIntDec, vData: uint32(verCode)},
		{nameIdx: 3, rawVal: 8, vType: typeString, vData: 8},
	})
	sdkElem := makeStartElem(4, []attr{
		{nameIdx: 5, rawVal: 0xffffffff, vType: typeIntDec, vData: uint32(minSDK)},
		{nameIdx: 6, rawVal: 0xffffffff, vType: typeIntDec, vData: uint32(targetSDK)},
	})

	totalSize := 8 + sp.Len() + resBuf.Len() + len(manifestElem) + len(sdkElem)
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(chunkXML))
	_ = binary.Write(&out, binary.LittleEndian, uint16(8))
	_ = binary.Write(&out, binary.LittleEndian, uint32(totalSize))
	out.Write(sp.Bytes())
	out.Write(resBuf.Bytes())
	out.Write(manifestElem)
	out.Write(sdkElem)
	return out.Bytes()
}

func generateTestCertDER(t *testing.T, cn string) ([]byte, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return der, hex.EncodeToString(sum[:])
}

func u32lp(b []byte) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(b)))
	out.Write(b)
	return out.Bytes()
}

// buildV3SignedAPK writes a valid ZIP containing AndroidManifest.xml and injects
// a genuine APK Signature Scheme v3 block immediately before the Central Directory.
func buildV3SignedAPK(t *testing.T, path, pkg, verName string, verCode, minSDK, targetSDK int32, certDER []byte) {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, err := zw.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(buildMinimalAXML(pkg, verName, verCode, minSDK, targetSDK)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	rawZip := zipBuf.Bytes()

	// Locate EOCD at the end of rawZip
	eocd := bytes.LastIndex(rawZip, []byte{0x50, 0x4b, 0x05, 0x06})
	if eocd < 0 {
		t.Fatal("missing EOCD in generated zip")
	}
	cdOffset := int(binary.LittleEndian.Uint32(rawZip[eocd+16:]))

	// Build v3 signer value: signers > signer > signedData(digests, certs)
	digests := u32lp(nil)
	certs := u32lp(u32lp(certDER))
	signedData := u32lp(append(digests, certs...))
	signer := u32lp(signedData)
	v3Val := u32lp(signer)

	var pair bytes.Buffer
	_ = binary.Write(&pair, binary.LittleEndian, uint64(4+len(v3Val)))
	_ = binary.Write(&pair, binary.LittleEndian, uint32(idV3))
	pair.Write(v3Val)

	blockSizeField := uint64(pair.Len() + 24)
	var sigBlock bytes.Buffer
	_ = binary.Write(&sigBlock, binary.LittleEndian, blockSizeField)
	sigBlock.Write(pair.Bytes())
	_ = binary.Write(&sigBlock, binary.LittleEndian, blockSizeField)
	_ = binary.Write(&sigBlock, binary.LittleEndian, uint64(sigBlockMagicLo))
	_ = binary.Write(&sigBlock, binary.LittleEndian, uint64(sigBlockMagicHi))

	var final bytes.Buffer
	final.Write(rawZip[:cdOffset])
	newCDOffset := uint32(final.Len() + sigBlock.Len())
	final.Write(sigBlock.Bytes())
	eocdRel := eocd - cdOffset
	tail := append([]byte(nil), rawZip[cdOffset:]...)
	binary.LittleEndian.PutUint32(tail[eocdRel+16:], newCDOffset)
	final.Write(tail)

	if err := os.WriteFile(path, final.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInspectManifestAndV3Cert(t *testing.T) {
	der, wantCertHex := generateTestCertDER(t, "Android Debug")
	apkPath := filepath.Join(t.TempDir(), "test-v3.apk")
	buildV3SignedAPK(t, apkPath, "com.example.rocket", "2.4.1-debug", 20401, 26, 35, der)

	ins, err := Inspect(apkPath)
	if err != nil {
		t.Fatalf("Inspect error: %v", err)
	}
	if !ins.HaveInfo {
		t.Fatalf("expected HaveInfo=true, err=%s", ins.InfoErr)
	}
	if ins.Info.Package != "com.example.rocket" {
		t.Errorf("Package = %q, want com.example.rocket", ins.Info.Package)
	}
	if ins.Info.VersionCode != 20401 || ins.Info.VersionName != "2.4.1-debug" {
		t.Errorf("Version = %s (%d), want 2.4.1-debug (20401)", ins.Info.VersionName, ins.Info.VersionCode)
	}
	if ins.Info.MinSDK != 26 || ins.Info.TargetSDK != 35 {
		t.Errorf("SDK = min %d target %d, want 26/35", ins.Info.MinSDK, ins.Info.TargetSDK)
	}
	if ins.Cert.SHA256 != wantCertHex || ins.Cert.Scheme != "v3" || ins.Cert.Subject != "Android Debug" {
		t.Errorf("Cert = %+v, want sha=%s scheme=v3 subject=Android Debug", ins.Cert, wantCertHex)
	}
	// Self-computed SHA-256 on untrusted file must start as DigestUnverified!
	if ins.DigestStatus != DigestUnverified {
		t.Errorf("Inspect without trusted digest must report DigestUnverified, got %q", ins.DigestStatus)
	}
}

func TestVerifyExpectedSHA256Honesty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "payload.bin")
	data := []byte("trusted-release-bytes")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])

	// 1. No expected digest -> DigestUnverified (never DigestVerified)
	st, got, err := VerifyExpectedSHA256(p, "")
	if err != nil || st != DigestUnverified || got != hexSum {
		t.Fatalf("empty expected digest: status=%q got=%q err=%v", st, got, err)
	}

	// 2. Matching GitHub sha256:... -> DigestVerified
	st, _, err = VerifyExpectedSHA256(p, "sha256:"+strings.ToUpper(hexSum))
	if err != nil || st != DigestVerified {
		t.Fatalf("matching expected digest: status=%q err=%v", st, err)
	}

	// 3. Mismatched expected digest -> DigestMismatch + error
	st, _, err = VerifyExpectedSHA256(p, "sha256:"+strings.Repeat("0", 64))
	if err == nil || st != DigestMismatch {
		t.Fatalf("mismatched expected digest must fail: status=%q err=%v", st, err)
	}

	// 4. Malformed expected digest -> DigestMismatch + error
	st, _, err = VerifyExpectedSHA256(p, "md5:1234")
	if err == nil || st != DigestMismatch {
		t.Fatalf("unsupported digest algorithm must fail: status=%q err=%v", st, err)
	}
}

func TestEvaluateSafetyChecksAndDowngradePolicy(t *testing.T) {
	ins := Inspection{
		HaveInfo: true,
		Info: Info{
			Package:     "com.example.other",
			VersionCode: 100,
			VersionName: "1.0.0",
			MinSDK:      34,
		},
		Cert: Cert{SHA256: strings.Repeat("a", 64), Scheme: "v3"},
	}

	// Package mismatch + Signature mismatch + Downgrade under "auto" + MinSDK too high
	res := EvaluateSafety(ins, SafetyInput{
		ExpectedPackage: "com.example.app",
		InstalledFound:  true,
		InstalledVCode:  120,
		InstalledVName:  "1.2.0",
		InstalledCert:   strings.Repeat("b", 64),
		DeviceSDK:       31,
		DowngradeMode:   "auto",
		CheckPackage:    true,
		CheckSignature:  true,
	})
	if res.AllowDowngrade {
		t.Fatal("downgrade=auto must NEVER silently set AllowDowngrade=true")
	}
	if !res.IsDowngrade || !res.NeedsConfirm {
		t.Fatalf("expected IsDowngrade=true and NeedsConfirm=true, got %+v", res)
	}
	if len(res.Warnings) != 4 {
		t.Fatalf("expected 4 warnings (pkg, sig, downgrade, minsdk), got %d: %+v", len(res.Warnings), res.Warnings)
	}

	// Downgrade under "never" blocks outright
	blocked := EvaluateSafety(ins, SafetyInput{
		ExpectedPackage: "com.example.other",
		InstalledFound:  true,
		InstalledVCode:  120,
		InstalledVName:  "1.2.0",
		InstalledCert:   strings.Repeat("a", 64),
		DeviceSDK:       35,
		DowngradeMode:   "never",
		CheckPackage:    true,
		CheckSignature:  true,
	})
	if blocked.BlockedReason == "" {
		t.Fatalf("expected BlockedReason under downgrade=never, got %+v", blocked)
	}

	// Downgrade under explicit "allow" sets AllowDowngrade=true without blocking
	allowed := EvaluateSafety(ins, SafetyInput{
		ExpectedPackage: "com.example.other",
		InstalledFound:  true,
		InstalledVCode:  120,
		InstalledVName:  "1.2.0",
		InstalledCert:   strings.Repeat("a", 64),
		DeviceSDK:       35,
		DowngradeMode:   "allow",
		CheckPackage:    true,
		CheckSignature:  true,
	})
	if !allowed.AllowDowngrade || allowed.NeedsConfirm || allowed.BlockedReason != "" {
		t.Fatalf("expected clean AllowDowngrade=true under downgrade=allow, got %+v", allowed)
	}
}

func TestInstallSafetyVersionAndVariantScenarios(t *testing.T) {
	base := Inspection{HaveInfo: true, Info: Info{Package: "com.example.app", VersionCode: 100, VersionName: "2.0"}, Cert: Cert{SHA256: strings.Repeat("a", 64)}}
	cases := []struct {
		name     string
		in       SafetyInput
		relation string
		warning  WarningKind
	}{
		{"same code different name is still a choice", SafetyInput{InstalledFound: true, InstalledVCode: 100, InstalledVName: "debug"}, "reinstall", WarnSameVersion},
		{"older code", SafetyInput{InstalledFound: true, InstalledVCode: 101, DowngradeMode: "auto"}, "downgrade", WarnDowngrade},
		{"newer code", SafetyInput{InstalledFound: true, InstalledVCode: 99}, "upgrade", ""},
		{"version names alone are not Android version codes", SafetyInput{InstalledFound: true, InstalledVName: "9.0"}, "unknown", ""},
		{"unknown package status", SafetyInput{}, "unknown", ""},
		{"distinct variant application ID", SafetyInput{ExpectedPackage: "com.example.app.debug", InstalledQueryable: true, CheckPackage: true}, "fresh", WarnPackageMismatch},
		{"different key same ID", SafetyInput{InstalledFound: true, InstalledVCode: 99, InstalledCert: strings.Repeat("b", 64), CheckSignature: true}, "upgrade", WarnSignatureMismatch},
		{"history for another variant is not a key mismatch", SafetyInput{InstalledFound: true, InstalledVCode: 99, HistoryPackage: "com.example.app.debug", HistoryCert: strings.Repeat("b", 64), CheckSignature: true}, "upgrade", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateSafety(base, tc.in)
			if got.VersionRelation != tc.relation {
				t.Fatalf("relation=%s want %s", got.VersionRelation, tc.relation)
			}
			present := false
			for _, w := range got.Warnings {
				if w.Kind == tc.warning {
					present = true
				}
			}
			if tc.warning != "" && !present {
				t.Fatalf("missing %s: %+v", tc.warning, got)
			}
			if tc.warning == "" && len(got.Warnings) != 0 {
				t.Fatalf("unexpected warning %+v", got.Warnings)
			}
		})
	}
}
