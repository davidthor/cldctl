package native

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCryptoRSAKey_Default2048(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoRSAKey("test-rsa", map[string]interface{}{
		"name": "test-rsa",
	})
	require.NoError(t, err)
	require.NotNil(t, rs)

	assert.Equal(t, "crypto:rsa_key", rs.Type)
	assert.Equal(t, "test-rsa", rs.ID)

	// Verify private key PEM is valid RSA
	privPEM := rs.Outputs["private_key_pem"].(string)
	block, _ := pem.Decode([]byte(privPEM))
	require.NotNil(t, block, "expected valid PEM block for private key")
	assert.Equal(t, "RSA PRIVATE KEY", block.Type)

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, 2048, privKey.N.BitLen())

	// Verify public key PEM is valid
	pubPEM := rs.Outputs["public_key_pem"].(string)
	pubBlock, _ := pem.Decode([]byte(pubPEM))
	require.NotNil(t, pubBlock, "expected valid PEM block for public key")
	assert.Equal(t, "PUBLIC KEY", pubBlock.Type)

	pubKey, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	require.NoError(t, err)
	_, ok := pubKey.(*rsa.PublicKey)
	assert.True(t, ok, "expected RSA public key")

	// Verify base64 outputs decode to the same PEM
	privB64 := rs.Outputs["private_key_base64"].(string)
	decoded, err := base64.StdEncoding.DecodeString(privB64)
	require.NoError(t, err)
	assert.Equal(t, privPEM, string(decoded))

	pubB64 := rs.Outputs["public_key_base64"].(string)
	decoded, err = base64.StdEncoding.DecodeString(pubB64)
	require.NoError(t, err)
	assert.Equal(t, pubPEM, string(decoded))
}

func TestApplyCryptoRSAKey_4096Bits(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoRSAKey("test-rsa-4096", map[string]interface{}{
		"name": "test-rsa-4096",
		"bits": 4096,
	})
	require.NoError(t, err)

	block, _ := pem.Decode([]byte(rs.Outputs["private_key_pem"].(string)))
	require.NotNil(t, block)
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, 4096, privKey.N.BitLen())
}

func TestApplyCryptoRSAKey_BitsAsString(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoRSAKey("test-rsa-str", map[string]interface{}{
		"name": "test",
		"bits": "2048",
	})
	require.NoError(t, err)

	block, _ := pem.Decode([]byte(rs.Outputs["private_key_pem"].(string)))
	require.NotNil(t, block)
	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, 2048, privKey.N.BitLen())
}

func TestApplyCryptoECDSAKey_DefaultP256(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoECDSAKey("test-ecdsa", map[string]interface{}{
		"name": "test-ecdsa",
	})
	require.NoError(t, err)
	require.NotNil(t, rs)

	assert.Equal(t, "crypto:ecdsa_key", rs.Type)

	// Verify private key PEM is valid ECDSA
	privPEM := rs.Outputs["private_key_pem"].(string)
	block, _ := pem.Decode([]byte(privPEM))
	require.NotNil(t, block, "expected valid PEM block")
	assert.Equal(t, "EC PRIVATE KEY", block.Type)

	privKey, err := x509.ParseECPrivateKey(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, "P-256", privKey.Curve.Params().Name)

	// Verify public key PEM
	pubPEM := rs.Outputs["public_key_pem"].(string)
	pubBlock, _ := pem.Decode([]byte(pubPEM))
	require.NotNil(t, pubBlock)
	pubKey, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	require.NoError(t, err)
	ecPub, ok := pubKey.(*ecdsa.PublicKey)
	assert.True(t, ok, "expected ECDSA public key")
	assert.Equal(t, "P-256", ecPub.Curve.Params().Name)
}

func TestApplyCryptoECDSAKey_P384(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoECDSAKey("test-p384", map[string]interface{}{
		"name":  "test-p384",
		"curve": "P-384",
	})
	require.NoError(t, err)

	block, _ := pem.Decode([]byte(rs.Outputs["private_key_pem"].(string)))
	require.NotNil(t, block)
	privKey, err := x509.ParseECPrivateKey(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, "P-384", privKey.Curve.Params().Name)
}

func TestApplyCryptoECDSAKey_UnsupportedCurve(t *testing.T) {
	p := &Plugin{}
	_, err := p.applyCryptoECDSAKey("test-bad", map[string]interface{}{
		"name":  "test-bad",
		"curve": "P-192",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported ECDSA curve")
}

func TestApplyCryptoSymmetricKey_Default256Bits(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoSymmetricKey("test-sym", map[string]interface{}{
		"name": "test-sym",
	})
	require.NoError(t, err)
	require.NotNil(t, rs)

	assert.Equal(t, "crypto:symmetric_key", rs.Type)

	// Default 256 bits = 32 bytes = 64 hex chars
	hexKey := rs.Outputs["key"].(string)
	assert.Len(t, hexKey, 64, "expected 64 hex chars for 256-bit key")

	// Base64 output should decode to 32 bytes
	b64Key := rs.Outputs["key_base64"].(string)
	decoded, err := base64.StdEncoding.DecodeString(b64Key)
	require.NoError(t, err)
	assert.Len(t, decoded, 32, "expected 32 bytes for 256-bit key")
}

func TestApplyCryptoSymmetricKey_128Bits(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoSymmetricKey("test-128", map[string]interface{}{
		"name": "test-128",
		"bits": 128,
	})
	require.NoError(t, err)

	hexKey := rs.Outputs["key"].(string)
	assert.Len(t, hexKey, 32, "expected 32 hex chars for 128-bit key")

	decoded, err := base64.StdEncoding.DecodeString(rs.Outputs["key_base64"].(string))
	require.NoError(t, err)
	assert.Len(t, decoded, 16, "expected 16 bytes for 128-bit key")
}

func TestApplyCryptoSymmetricKey_BitsAsString(t *testing.T) {
	p := &Plugin{}
	rs, err := p.applyCryptoSymmetricKey("test-str", map[string]interface{}{
		"name": "test-str",
		"bits": "256",
	})
	require.NoError(t, err)

	hexKey := rs.Outputs["key"].(string)
	assert.Len(t, hexKey, 64, "expected 64 hex chars for 256-bit key")
}

func TestApplyCryptoKeys_UniquePerCall(t *testing.T) {
	p := &Plugin{}

	rs1, err := p.applyCryptoSymmetricKey("key1", map[string]interface{}{"name": "key1"})
	require.NoError(t, err)

	rs2, err := p.applyCryptoSymmetricKey("key2", map[string]interface{}{"name": "key2"})
	require.NoError(t, err)

	assert.NotEqual(t, rs1.Outputs["key"], rs2.Outputs["key"],
		"each call should generate a unique key")
}
