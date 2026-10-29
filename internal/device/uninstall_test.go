package device

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
)

func TestDumpsysDERCertificateFingerprint(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	out := "versionCode=1\nsignatures=PackageSignatures{aa version:3, signatures:[" + hex.EncodeToString(der) + "], past signatures:[]}"
	app, found := parseDumpsysPackage("com.example.app", out)
	if !found || app.CertSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("app=%+v", app)
	}
	app, _ = parseDumpsysPackage("com.example.app", "versionCode=1 signatures:["+strings.Repeat("ab", 64)+"]")
	if app.CertSHA256 != "" {
		t.Fatal("non-certificate bytes must not become a trusted certificate")
	}
}

func TestUninstallRequiresExactLiveAppAndCurrentUser(t *testing.T) {
	original := strings.Repeat("a", 64)
	target := strings.Repeat("b", 64)
	for _, tc := range []struct {
		name                  string
		current, listed, cert string
		success               bool
	}{
		{"confirmed", fmt.Sprint(os.Getuid() / 100000), "package:com.example.app", original, true},
		{"different user", "99999", "package:com.example.app", original, false},
		{"different package", fmt.Sprint(os.Getuid() / 100000), "package:com.example.app.debug", original, false},
		{"changed signer", fmt.Sprint(os.Getuid() / 100000), "package:com.example.app", target, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uninstalled := false
			withDeviceStubs(t, true, map[string]bool{"rish": true}, nil, func(ctx context.Context, name string, args ...string) (string, error) {
				cmd := strings.Join(args, " ")
				switch {
				case strings.HasSuffix(cmd, "id"):
					return "uid=2000(shell)", nil
				case strings.Contains(cmd, "get-current-user"):
					return tc.current, nil
				case strings.Contains(cmd, "list packages"):
					return tc.listed, nil
				case strings.Contains(cmd, "dumpsys package"):
					return "versionCode=100 cert sha256: " + tc.cert, nil
				case strings.Contains(cmd, "pm uninstall"):
					uninstalled = true
					want := fmt.Sprintf("pm uninstall --user %d com.example.app", os.Getuid()/100000)
					if !strings.Contains(cmd, want) {
						t.Errorf("unsafe uninstall command %s", cmd)
					}
					return "Success", nil
				default:
					return "", errors.New("unexpected command")
				}
			})
			err := UninstallMismatchedRishContext(context.Background(), "com.example.app", original, target)
			if (err == nil) != tc.success || uninstalled != tc.success {
				t.Fatalf("err=%v uninstalled=%v", err, uninstalled)
			}
		})
	}
}

func TestPackageListRequiresExactPackage(t *testing.T) {
	if _, ok := parsePackageList("com.example.app", "package:com.example.app.debug versionCode:100"); ok {
		t.Fatal("matched another variant")
	}
	app, ok := parsePackageList("com.example.app", "package:com.example.app versionCode:100")
	if !ok || app.VersionCode != 100 {
		t.Fatalf("%+v", app)
	}
}
