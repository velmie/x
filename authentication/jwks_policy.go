package authentication

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"filippo.io/edwards25519"
	"github.com/go-jose/go-jose/v3"
)

// JWKSVerificationPolicy opts into public JWT verification keys. Supported
// algorithms are RS256/384/512, PS256/384/512, ES256/384/512, and EdDSA (Ed25519).
// RSA keys must be at least 2048 bits. A key without alg must match exactly one
// allowed algorithm. Key usage metadata, when present, must permit verification.
type JWKSVerificationPolicy struct {
	// AllowedAlgorithms must contain at least one supported algorithm.
	AllowedAlgorithms []string
}

type jwksSnapshot struct {
	keys       map[string]crypto.PublicKey
	algorithms map[string]string
	rejected   map[string]struct{}
}

func (p *JWKSVerificationPolicy) validate() error {
	if len(p.AllowedAlgorithms) == 0 {
		return rejectedJWK("config", "missing_algorithms", "VerificationPolicy.AllowedAlgorithms", nil)
	}
	for i, algorithm := range p.AllowedAlgorithms {
		switch algorithm {
		case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA":
		default:
			return rejectedJWK("config", "unsupported_algorithm", fmt.Sprintf("VerificationPolicy.AllowedAlgorithms[%d]", i), nil)
		}
	}
	return nil
}

func decodeJWKSSnapshot(rawKeys []byte, policy *JWKSVerificationPolicy) (snapshot *jwksSnapshot, err error) {
	keyIndex := -1
	defer func() {
		if diagnostic, ok := err.(*JWKSError); ok && keyIndex >= 0 {
			path := fmt.Sprintf("keys[%d]", keyIndex)
			if diagnostic.field != "" {
				path += "." + diagnostic.field
			}
			diagnostic.field = path
		}
	}()
	snapshot = &jwksSnapshot{keys: make(map[string]crypto.PublicKey)}
	var keys []json.RawMessage
	if err := json.Unmarshal(rawKeys, &keys); err != nil {
		if policy == nil {
			return nil, &JWKSError{stage: "decode", reason: "invalid_keys", field: "keys", cause: err}
		}
		return nil, rejectedJWK("decode", "invalid_keys", "keys", err)
	}
	if policy != nil {
		snapshot.algorithms = make(map[string]string)
		snapshot.rejected = make(map[string]struct{})
	}
	for i, raw := range keys {
		keyIndex = i
		if policy == nil {
			var key jose.JSONWebKey
			if err := json.Unmarshal(raw, &key); err != nil {
				return nil, &JWKSError{stage: "decode", reason: "invalid_keys", cause: err}
			}
			value := key.Key
			if deriver, ok := value.(interface{ Public() crypto.PublicKey }); ok {
				value = deriver.Public()
			}
			snapshot.keys[key.KeyID] = value
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return nil, rejectedJWK("decode", "invalid_key", "", err)
		}
		kid, err := jwkString(fields, "kid")
		if err != nil {
			return nil, err
		}
		use, err := jwkString(fields, "use")
		if err != nil {
			return nil, err
		}
		algorithm, err := jwkString(fields, "alg")
		if err != nil {
			return nil, err
		}
		eligible, err := verificationUsage(use, fields["key_ops"])
		if err != nil {
			return nil, err
		}
		if !eligible || algorithm != "" && !allowedJWKAlgorithm(policy, algorithm) {
			snapshot.rejected[kid] = struct{}{}
			continue
		}
		for _, parameter := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"} {
			if _, present := fields[parameter]; present {
				return nil, rejectedJWK("validate", "private_or_symmetric_key", parameter, nil)
			}
		}
		var key jose.JSONWebKey
		if err := json.Unmarshal(raw, &key); err != nil {
			return nil, rejectedJWK("decode", "invalid_key", "", err)
		}
		if err := validateVerificationKey(key.Key, fields); err != nil {
			return nil, err
		}
		if algorithm == "" {
			for _, candidate := range policy.AllowedAlgorithms {
				if matchesKeyAlgorithm(key.Key, candidate) {
					if algorithm != "" && algorithm != candidate {
						return nil, rejectedJWK("validate", "ambiguous_algorithm", "alg", nil)
					}
					algorithm = candidate
				}
			}
			if algorithm == "" {
				snapshot.rejected[kid] = struct{}{}
				continue
			}
		} else if !matchesKeyAlgorithm(key.Key, algorithm) {
			return nil, rejectedJWK("validate", "incompatible_algorithm", "alg", nil)
		}
		if _, exists := snapshot.keys[kid]; exists {
			return nil, rejectedJWK("validate", "duplicate_key_id", "kid", nil)
		}
		snapshot.keys[kid] = key.Key
		snapshot.algorithms[kid] = algorithm
	}
	keyIndex = -1
	if len(keys) > 0 && len(snapshot.keys) == 0 {
		return nil, rejectedJWK("validate", "no_verification_keys", "keys", nil)
	}
	return snapshot, nil
}

func jwkString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, exists := fields[name]
	if !exists {
		return "", nil
	}
	raw = bytes.TrimSpace(raw)
	var value string
	if len(raw) == 0 || raw[0] != '"' {
		return "", rejectedJWK("validate", "invalid_metadata", name, nil)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", rejectedJWK("validate", "invalid_metadata", name, err)
	}
	if name != "kid" && value == "" {
		return "", rejectedJWK("validate", "invalid_metadata", name, nil)
	}
	return value, nil
}

func verificationUsage(use string, raw json.RawMessage) (bool, error) {
	if raw == nil {
		return use == "" || use == "sig", nil
	}
	raw = bytes.TrimSpace(raw)
	var operations []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' {
		return false, rejectedJWK("validate", "invalid_key_operations", "key_ops", nil)
	}
	if err := json.Unmarshal(raw, &operations); err != nil {
		return false, rejectedJWK("validate", "invalid_key_operations", "key_ops", err)
	}
	seen := make(map[string]struct{}, len(operations))
	verify, otherOperation := false, false
	for i, rawOperation := range operations {
		var operation string
		rawOperation = bytes.TrimSpace(rawOperation)
		if len(rawOperation) == 0 || rawOperation[0] != '"' {
			return false, rejectedJWK("validate", "invalid_key_operations", fmt.Sprintf("key_ops[%d]", i), nil)
		}
		if err := json.Unmarshal(rawOperation, &operation); err != nil {
			return false, rejectedJWK("validate", "invalid_key_operations", fmt.Sprintf("key_ops[%d]", i), err)
		}
		if _, duplicate := seen[operation]; duplicate {
			return false, rejectedJWK("validate", "duplicate_key_operation", fmt.Sprintf("key_ops[%d]", i), nil)
		}
		seen[operation] = struct{}{}
		signing := operation == "sign" || operation == "verify"
		if use == "sig" && !signing || use == "enc" && signing {
			return false, rejectedJWK("validate", "inconsistent_key_usage", fmt.Sprintf("key_ops[%d]", i), nil)
		}
		verify = verify || operation == "verify"
		otherOperation = otherOperation || !signing
	}
	if verify && otherOperation {
		return false, rejectedJWK("validate", "inconsistent_key_usage", "key_ops", nil)
	}
	return (use == "" || use == "sig") && verify, nil
}

func validateVerificationKey(key crypto.PublicKey, fields map[string]json.RawMessage) error {
	switch key := key.(type) {
	case *rsa.PublicKey:
		// Check the encoded exponent too: the JWK decoder converts it to int.
		var exponent string
		if err := json.Unmarshal(fields["e"], &exponent); err != nil {
			return rejectedJWK("validate", "invalid_rsa_key", "e", err)
		}
		if len(exponent) > 6 {
			return rejectedJWK("validate", "invalid_rsa_key", "e", nil)
		}
		encoded, err := base64.RawURLEncoding.DecodeString(exponent)
		if err != nil {
			return rejectedJWK("validate", "invalid_rsa_key", "e", err)
		}
		var value uint64
		for _, b := range encoded {
			value = value<<8 | uint64(b)
		}
		if key.N == nil || key.N.BitLen() < 2048 || key.N.Bit(0) == 0 {
			return rejectedJWK("validate", "invalid_rsa_key", "n", nil)
		}
		if value < 3 || value > 1<<31-1 || value&1 == 0 || uint64(key.E) != value {
			return rejectedJWK("validate", "invalid_rsa_key", "e", nil)
		}
	case *ecdsa.PublicKey:
		if key.Curve == nil || key.X == nil || key.Y == nil || !key.Curve.IsOnCurve(key.X, key.Y) {
			return rejectedJWK("validate", "invalid_ec_key", "", nil)
		}
	case ed25519.PublicKey:
		// The JWK decoder copies x into a fixed-size slice, padding or truncating
		// invalid lengths. Validate the encoded material before trusting that slice.
		var coordinate string
		if err := json.Unmarshal(fields["x"], &coordinate); err != nil {
			return rejectedJWK("validate", "invalid_ed25519_key", "x", err)
		}
		if len(coordinate) != base64.RawURLEncoding.EncodedLen(ed25519.PublicKeySize) {
			return rejectedJWK("validate", "invalid_ed25519_key", "x", nil)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(coordinate)
		if err != nil {
			return rejectedJWK("validate", "invalid_ed25519_key", "x", err)
		}
		if len(decoded) != ed25519.PublicKeySize || len(key) != ed25519.PublicKeySize {
			return rejectedJWK("validate", "invalid_ed25519_key", "x", nil)
		}
		point, err := new(edwards25519.Point).SetBytes(decoded)
		if err != nil {
			return rejectedJWK("validate", "invalid_ed25519_key", "x", err)
		}
		// Small-order points can accept signatures without a private key.
		if !bytes.Equal(point.Bytes(), decoded) || new(edwards25519.Point).MultByCofactor(point).Equal(edwards25519.NewIdentityPoint()) == 1 {
			return rejectedJWK("validate", "invalid_ed25519_key", "x", nil)
		}
	default:
		return rejectedJWK("validate", "unsupported_key_type", "kty", nil)
	}
	return nil
}

func allowedJWKAlgorithm(policy *JWKSVerificationPolicy, algorithm string) bool {
	for _, allowed := range policy.AllowedAlgorithms {
		if algorithm == allowed {
			return true
		}
	}
	return false
}

func matchesKeyAlgorithm(key crypto.PublicKey, algorithm string) bool {
	switch key := key.(type) {
	case *rsa.PublicKey:
		switch algorithm {
		case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512":
			return true
		}
	case *ecdsa.PublicKey:
		switch key.Curve.Params().BitSize {
		case 256:
			return algorithm == "ES256"
		case 384:
			return algorithm == "ES384"
		case 521:
			return algorithm == "ES512"
		}
	case ed25519.PublicKey:
		return algorithm == "EdDSA"
	}
	return false
}

func rejectedJWK(stage, reason, field string, cause error) error {
	return &JWKSError{stage: stage, reason: reason, field: field, cause: errors.Join(ErrJWKSKeyRejected, cause)}
}
