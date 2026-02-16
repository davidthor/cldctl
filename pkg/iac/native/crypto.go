package native

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strconv"
)

// applyCryptoRSAKey generates an RSA key pair and returns PEM + base64 outputs.
func (p *Plugin) applyCryptoRSAKey(name string, props map[string]interface{}) (*ResourceState, error) {
	bits := 2048
	if v, ok := props["bits"]; ok {
		switch b := v.(type) {
		case int:
			bits = b
		case float64:
			bits = int(b)
		case string:
			if n, err := strconv.Atoi(b); err == nil {
				bits = n
			}
		}
	}

	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RSA public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	return &ResourceState{
		Type:       "crypto:rsa_key",
		ID:         name,
		Properties: props,
		Outputs: map[string]interface{}{
			"private_key_pem":    string(privPEM),
			"public_key_pem":     string(pubPEM),
			"private_key_base64": base64.StdEncoding.EncodeToString(privPEM),
			"public_key_base64":  base64.StdEncoding.EncodeToString(pubPEM),
		},
	}, nil
}

// applyCryptoECDSAKey generates an ECDSA key pair and returns PEM + base64 outputs.
func (p *Plugin) applyCryptoECDSAKey(name string, props map[string]interface{}) (*ResourceState, error) {
	curveName := "P-256"
	if v, ok := props["curve"]; ok {
		if s, ok := v.(string); ok && s != "" {
			curveName = s
		}
	}

	var curve elliptic.Curve
	switch curveName {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported ECDSA curve: %s (supported: P-256, P-384, P-521)", curveName)
	}

	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ECDSA key: %w", err)
	}

	privDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ECDSA private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ECDSA public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	return &ResourceState{
		Type:       "crypto:ecdsa_key",
		ID:         name,
		Properties: props,
		Outputs: map[string]interface{}{
			"private_key_pem":    string(privPEM),
			"public_key_pem":     string(pubPEM),
			"private_key_base64": base64.StdEncoding.EncodeToString(privPEM),
			"public_key_base64":  base64.StdEncoding.EncodeToString(pubPEM),
		},
	}, nil
}

// applyCryptoSymmetricKey generates a random symmetric key and returns hex + base64 outputs.
func (p *Plugin) applyCryptoSymmetricKey(name string, props map[string]interface{}) (*ResourceState, error) {
	bits := 256
	if v, ok := props["bits"]; ok {
		switch b := v.(type) {
		case int:
			bits = b
		case float64:
			bits = int(b)
		case string:
			if n, err := strconv.Atoi(b); err == nil {
				bits = n
			}
		}
	}

	byteLen := bits / 8
	if byteLen < 1 {
		byteLen = 32
	}

	keyBytes := make([]byte, byteLen)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate symmetric key: %w", err)
	}

	hexKey := fmt.Sprintf("%x", keyBytes)

	return &ResourceState{
		Type:       "crypto:symmetric_key",
		ID:         name,
		Properties: props,
		Outputs: map[string]interface{}{
			"key":        hexKey,
			"key_base64": base64.StdEncoding.EncodeToString(keyBytes),
		},
	}, nil
}
