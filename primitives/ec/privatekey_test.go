package primitives

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	shamir "github.com/bitcoin-sv/go-sdk/primitives/shamir"
	"github.com/stretchr/testify/require"
)

func TestPrivKeys(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
	}{
		{
			name: "check curve",
			key: []byte{
				0xea, 0xf0, 0x2c, 0xa3, 0x48, 0xc5, 0x24, 0xe6,
				0x39, 0x26, 0x55, 0xba, 0x4d, 0x29, 0x60, 0x3c,
				0xd1, 0xa7, 0x34, 0x7d, 0x9d, 0x65, 0xcf, 0xe9,
				0x3c, 0xe1, 0xeb, 0xff, 0xdc, 0xa2, 0x26, 0x94,
			},
		},
	}

	for _, test := range tests {
		priv, pub := PrivateKeyFromBytes(test.key)

		_, err := ParsePubKey(pub.SerializeUncompressed())
		if err != nil {
			t.Errorf("%s privkey: %v", test.name, err)
			continue
		}

		hash := []byte{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}
		sig, err := priv.Sign(hash)
		if err != nil {
			t.Errorf("%s could not sign: %v", test.name, err)
			continue
		}

		if !sig.Verify(hash, pub) {
			t.Errorf("%s could not verify: %v", test.name, err)
			continue
		}

		serializedKey := priv.Serialize()
		if !bytes.Equal(serializedKey, test.key) {
			t.Errorf("%s unexpected serialized bytes - got: %x, "+
				"want: %x", test.name, serializedKey, test.key)
		}
	}
}

// Test vector struct
type privateTestVector struct {
	SenderPublicKey     string `json:"senderPublicKey"`
	RecipientPrivateKey string `json:"recipientPrivateKey"`
	InvoiceNumber       string `json:"invoiceNumber"`
	ExpectedPrivateKey  string `json:"privateKey"`
}

const createPolyFail = "Failed to create polynomial: %v"

func TestBRC42PrivateVectors(t *testing.T) {
	// Determine the directory of the current test file
	_, currentFile, _, ok := runtime.Caller(0)
	testdataPath := filepath.Join(filepath.Dir(currentFile), "testdata", "BRC42.private.vectors.json")

	require.True(t, ok, "Could not determine the directory of the current test file")

	// Read in the file
	vectors, err := os.ReadFile(testdataPath)
	if err != nil {
		t.Fatalf("Could not read test vectors: %v", err) // use Fatalf to stop test if file cannot be read
	}
	// unmarshal the json
	var testVectors []privateTestVector
	err = json.Unmarshal(vectors, &testVectors)
	if err != nil {
		t.Errorf("Could not unmarshal test vectors: %v", err)
	}
	for i, v := range testVectors {
		t.Run("BRC42 private vector #"+strconv.Itoa(i+1), func(t *testing.T) {
			publicKey, err := PublicKeyFromString(v.SenderPublicKey)
			if err != nil {
				t.Errorf("Could not parse public key: %v", err)
			}
			privateKey, err := PrivateKeyFromHex(v.RecipientPrivateKey)
			if err != nil {
				t.Errorf("Could not parse private key: %v", err)
			}
			derived, err := privateKey.DeriveChild(publicKey, v.InvoiceNumber)
			if err != nil {
				t.Errorf("Could not derive child key: %v", err)
			}

			// Convert derived private key to hex and compare
			derivedHex := hex.EncodeToString(derived.Serialize())
			if derivedHex != v.ExpectedPrivateKey {
				t.Errorf("Derived private key does not match expected: got %v, want %v", derivedHex, v.ExpectedPrivateKey)
			}
		})
	}
}

// TestPolynomialFromPrivateKey checks if a polynomial is correctly created from a private key
func TestPolynomialFromPrivateKey(t *testing.T) {

	pk, _ := NewPrivateKey()
	threshold := 3

	poly, err := pk.ToPolynomial(threshold)
	if err != nil {
		t.Fatalf(createPolyFail, err)
	}

	if len(poly.Points) != threshold {
		t.Errorf("Incorrect number of points. Expected %d, got %d", threshold, len(poly.Points))
	}

	if poly.Points[0].X.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("First point x-coordinate should be 0, got %v", poly.Points[0].X)
	}

	if poly.Points[0].Y.Cmp(pk.D) != 0 {
		t.Errorf("First point y-coordinate should be the key, got %v", poly.Points[0].Y)
	}

	// Check for uniqueness of x-coordinates
	xCoords := make(map[string]bool)
	for _, point := range poly.Points {
		if xCoords[point.X.String()] {
			t.Errorf("Duplicate x-coordinate found: %v", point.X)
		}
		xCoords[point.X.String()] = true
	}
}

func TestPolynomialFullProcess(t *testing.T) {
	// Create a private key
	privateKey, err := NewPrivateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	threshold := 3
	totalShares := 5

	// Generate the polynomial
	poly, err := privateKey.ToPolynomial(threshold)
	if err != nil {
		t.Fatalf(createPolyFail, err)
	}

	// Log the generated polynomial points
	t.Logf("Generated polynomial points:")
	for i, point := range poly.Points {
		t.Logf("Point %d: (%v, %v)", i, point.X, point.Y)
	}

	// Generate shares
	shares := make([]*shamir.PointInFiniteField, totalShares)
	t.Logf("Generated shares:")
	for i := 0; i < totalShares; i++ {
		x := big.NewInt(int64(i + 1))
		y := poly.ValueAt(x)
		shares[i] = shamir.NewPointInFiniteField(x, y)
		t.Logf("Share %d: (%v, %v)", i, shares[i].X, shares[i].Y)
	}

	// Reconstruct the secret using threshold number of shares
	reconstructPoly := shamir.NewPolynomial(shares[:threshold], threshold)
	reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))

	t.Logf("Original secret: %v", privateKey.D)
	t.Logf("Reconstructed secret: %v", reconstructedSecret)

	if reconstructedSecret.Cmp(privateKey.D) != 0 {
		t.Errorf("Secret reconstruction failed. Expected %v, got %v", privateKey.D, reconstructedSecret)
	}
}

func TestPolynomialDifferentThresholdsAndShares(t *testing.T) {
	testCases := []struct {
		threshold   int
		totalShares int
	}{
		{2, 3},
		{3, 5},
		{5, 10},
		{10, 20},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Threshold_%d_TotalShares_%d", tc.threshold, tc.totalShares), func(t *testing.T) {
			privateKey, _ := NewPrivateKey()
			poly, err := privateKey.ToPolynomial(tc.threshold)
			if err != nil {
				t.Fatalf(createPolyFail, err)
			}

			shares := make([]*shamir.PointInFiniteField, tc.totalShares)
			for i := 0; i < tc.totalShares; i++ {
				x := big.NewInt(int64(i + 1))
				y := poly.ValueAt(x)
				shares[i] = shamir.NewPointInFiniteField(x, y)
			}

			reconstructPoly := shamir.NewPolynomial(shares[:tc.threshold], tc.threshold)
			reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))

			if reconstructedSecret.Cmp(privateKey.D) != 0 {
				t.Errorf("Secret reconstruction failed. Expected %v, got %v", privateKey.D, reconstructedSecret)
			}
		})
	}
}

func TestPolynomialEdgeCases(t *testing.T) {
	privateKey, _ := NewPrivateKey()

	// Minimum threshold (2)
	t.Run("MinimumThreshold", func(t *testing.T) {
		threshold := 2
		totalShares := 3
		poly, _ := privateKey.ToPolynomial(threshold)
		shares := make([]*shamir.PointInFiniteField, totalShares)
		for i := 0; i < totalShares; i++ {
			x := big.NewInt(int64(i + 1))
			y := poly.ValueAt(x)
			shares[i] = shamir.NewPointInFiniteField(x, y)
		}
		reconstructPoly := shamir.NewPolynomial(shares[:threshold], threshold)
		reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))
		if reconstructedSecret.Cmp(privateKey.D) != 0 {
			t.Errorf("Secret reconstruction failed for minimum threshold")
		}
	})

	// Maximum threshold (total shares)
	t.Run("MaximumThreshold", func(t *testing.T) {
		threshold := 10
		totalShares := 10
		poly, _ := privateKey.ToPolynomial(threshold)
		shares := make([]*shamir.PointInFiniteField, totalShares)
		for i := 0; i < totalShares; i++ {
			x := big.NewInt(int64(i + 1))
			y := poly.ValueAt(x)
			shares[i] = shamir.NewPointInFiniteField(x, y)
		}
		reconstructPoly := shamir.NewPolynomial(shares, threshold)
		reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))
		if reconstructedSecret.Cmp(privateKey.D) != 0 {
			t.Errorf("Secret reconstruction failed for maximum threshold")
		}
	})
}

func TestPolynomialReconstructionWithDifferentSubsets(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	threshold := 3
	totalShares := 5

	poly, _ := privateKey.ToPolynomial(threshold)
	shares := make([]*shamir.PointInFiniteField, totalShares)
	for i := 0; i < totalShares; i++ {
		x := big.NewInt(int64(i + 1))
		y := poly.ValueAt(x)
		shares[i] = shamir.NewPointInFiniteField(x, y)
	}

	subsets := [][]int{
		{0, 1, 2},
		{1, 2, 3},
		{2, 3, 4},
		{0, 2, 4},
	}

	for i, subset := range subsets {
		t.Run(fmt.Sprintf("Subset_%d", i), func(t *testing.T) {
			subsetShares := make([]*shamir.PointInFiniteField, threshold)
			for j, idx := range subset {
				subsetShares[j] = shares[idx]
			}
			reconstructPoly := shamir.NewPolynomial(subsetShares, threshold)
			reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))
			if reconstructedSecret.Cmp(privateKey.D) != 0 {
				t.Errorf("Secret reconstruction failed for subset %v", subset)
			}
		})
	}
}

func TestPolynomialErrorHandling(t *testing.T) {
	privateKey, _ := NewPrivateKey()

	// Test with invalid threshold (too low)
	_, err := privateKey.ToPolynomial(1)
	if err == nil {
		t.Errorf("Expected error for threshold too low, got nil")
	}

	// Test reconstruction with insufficient shares
	threshold := 3
	poly, _ := privateKey.ToPolynomial(threshold)
	shares := make([]*shamir.PointInFiniteField, 2)
	for i := 0; i < 2; i++ {
		x := big.NewInt(int64(i + 1))
		y := poly.ValueAt(x)
		shares[i] = shamir.NewPointInFiniteField(x, y)
	}
	reconstructPoly := shamir.NewPolynomial(shares, 2)
	reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))
	if reconstructedSecret.Cmp(privateKey.D) == 0 {
		t.Errorf("Expected incorrect reconstruction with insufficient shares")
	}
}

func TestPolynomialConsistency(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	threshold := 3
	totalShares := 5

	for i := 0; i < 10; i++ {
		poly, _ := privateKey.ToPolynomial(threshold)
		shares := make([]*shamir.PointInFiniteField, totalShares)
		for j := 0; j < totalShares; j++ {
			x := big.NewInt(int64(j + 1))
			y := poly.ValueAt(x)
			shares[j] = shamir.NewPointInFiniteField(x, y)
		}
		reconstructPoly := shamir.NewPolynomial(shares[:threshold], threshold)
		reconstructedSecret := reconstructPoly.ValueAt(big.NewInt(0))
		if reconstructedSecret.Cmp(privateKey.D) != 0 {
			t.Errorf("Inconsistent secret reconstruction in run %d", i)
		}
	}
}

func TestPrivateKeyToKeyShares(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	threshold := 2
	totalShares := 5

	// it should split the private key into shares correctly
	shares, err := privateKey.ToKeyShares(threshold, totalShares)
	if err != nil {
		t.Fatalf("Failed to create initial key shares: %v", err)
	}

	backup, err := shares.ToBackupFormat()
	if err != nil {
		t.Fatalf("Failed to create backup format: %v", err)
	}

	if len(backup) != totalShares {
		t.Errorf("Incorrect number of shares. Expected %d, got %d", totalShares, len(backup))
	}

	if shares.Threshold != threshold {
		t.Errorf("Incorrect threshold. Expected %d, got %d", threshold, shares.Threshold)
	}

	// it should recombine the shares into a private key correctly
	for i := 0; i < 3; i++ {
		key, _ := NewPrivateKey()
		allShares, err := key.ToKeyShares(3, 5)
		if err != nil {
			t.Fatalf("Failed to create key shares: %v", err)
		}
		backup, _ := allShares.ToBackupFormat()
		log.Printf("backup: %v", backup)
		someShares, err := shamir.NewKeySharesFromBackupFormat(backup[:3])
		if err != nil {
			t.Fatalf("Failed to create key shares from backup format: %v", err)
		}
		rebuiltKey, err := PrivateKeyFromKeyShares(someShares)
		if err != nil {
			t.Fatalf("Failed to create private key from key shares: %v", err)
		}
		if !strings.EqualFold(rebuiltKey.Wif(), key.Wif()) {
			t.Errorf("Reconstructed key does not match original key")
		}
	}
}

// threshold should be between 2 and 99"
func TestThresholdLargerThanTotalShares(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	_, err := privateKey.ToKeyShares(100, 5)
	if err == nil {
		t.Errorf("Expected error for threshold must be less than total shares")
	}
}

func TestTotalSharesLessThanTwo(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	_, err := privateKey.ToKeyShares(2, 1)
	if err == nil {
		t.Errorf("Expected error for totalShares must be at least 2")
	}
}

func TestFewerPointsThanThreshold(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	shares, err := privateKey.ToKeyShares(3, 5)
	if err != nil {
		t.Fatalf("Failed to create key shares: %v", err)
	}

	shares.Points = shares.Points[:2]
	_, err = PrivateKeyFromKeyShares(shares)
	if err == nil {
		t.Errorf("Expected error for fewer points than threshold")
	}
}

// should throw an error for invalid threshold
func TestInvalidThreshold(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	_, err := privateKey.ToKeyShares(1, 2)
	if err == nil {
		t.Errorf("Expected error for threshold must be at least 2")
	}
}

// should throw an error for invalid totalShares
func TestInvalidTotalShares(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	_, err := privateKey.ToKeyShares(2, -4)
	if err == nil {
		t.Errorf("Expected error for totalShares must be at least 2")
	}
}

// should throw an error for totalShares being less than threshold
func TestTotalSharesLessThanThreshold(t *testing.T) {
	privateKey, _ := NewPrivateKey()
	_, err := privateKey.ToKeyShares(3, 2)
	if err == nil {
		t.Errorf("Expected error for threshold should be less than or equal to totalShares")
	}
}

// should throw an error if the same share is included twice during recovery
func TestSameShareTwiceDuringRecovery(t *testing.T) {
	backup := []string{
		"45s4vLL2hFvqmxrarvbRT2vZoQYGZGocsmaEksZ64o5M.A7nZrGux15nEsQGNZ1mbfnMKugNnS6SYYEQwfhfbDZG8.3.2f804d43",
		"7aPzkiGZgvU4Jira5PN9Qf9o7FEg6uwy1zcxd17NBhh3.CCt7NH1sPFgceb6phTRkfviim2WvmUycJCQd2BxauxP9.3.2f804d43",
		"9GaS2Tw5sXqqbuigdjwGPwPsQuEFqzqUXo5MAQhdK3es.8MLh2wyE3huyq6hiBXjSkJRucgyKh4jVY6ESq5jNtXRE.3.2f804d43",
		"GBmoNRbsMVsLmEK5A6G28fktUNonZkn9mDrJJ58FXgsf.HDBRkzVUCtZ38ApEu36fvZtDoDSQTv3TWmbnxwwR7kto.3.2f804d43",
		"2gHebXBgPd7daZbsj6w9TPDta3vQzqvbkLtJG596rdN1.E7ZaHyyHNDCwR6qxZvKkPPWWXzFCiKQFentJtvSSH5Bi.3.2f804d43",
	}
	recovery, err := shamir.NewKeySharesFromBackupFormat([]string{backup[0], backup[1], backup[1]})
	if err != nil {
		t.Fatalf("Failed to create key shares from backup format: %v", err)
	}
	_, err = PrivateKeyFromKeyShares(recovery)
	if err == nil {
		t.Errorf("Expected error for duplicate share detected, each must be unique")
	}
}

// should be able to create a backup array from a private key, and recover the same key back from the backup
func TestBackupAndRecovery(t *testing.T) {
	key, _ := NewPrivateKey()
	backup, err := key.ToBackupShares(3, 5)
	if err != nil {
		t.Fatalf("Failed to create backup shares: %v", err)
	}
	recoveredKey, err := PrivateKeyFromBackupShares(backup[:3])
	if err != nil {
		t.Fatalf("Failed to recover key from backup shares: %v", err)
	}
	if !bytes.Equal(recoveredKey.Serialize(), key.Serialize()) {
		t.Errorf("Recovered key does not match original key")
	}
}
