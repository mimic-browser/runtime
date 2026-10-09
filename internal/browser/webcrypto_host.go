package browser

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	_ "crypto/sha1"
	_ "crypto/sha256"
	_ "crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

type webCryptoRequest struct {
	Operation      string `json:"operation"`
	Name           string `json:"name"`
	Hash           string `json:"hash"`
	Key            []byte `json:"key"`
	Data           []byte `json:"data"`
	IV             []byte `json:"iv"`
	AdditionalData []byte `json:"additionalData"`
	Salt           []byte `json:"salt"`
	Info           []byte `json:"info"`
	Signature      []byte `json:"signature"`
	Length         int    `json:"length"`
	TagLength      int    `json:"tagLength"`
	Iterations     int    `json:"iterations"`
}

func webCryptoHash(name string) (crypto.Hash, error) {
	switch name {
	case "SHA-1":
		return crypto.SHA1, nil
	case "SHA-256":
		return crypto.SHA256, nil
	case "SHA-384":
		return crypto.SHA384, nil
	case "SHA-512":
		return crypto.SHA512, nil
	}
	return 0, fmt.Errorf("unsupported hash %q", name)
}

func installWebCryptoHost(host map[string]any, runtime engine.Runtime) {
	function := runtime.Function
	if borrowed, ok := runtime.(interface{ TransientFunction(engine.Function) any }); ok {
		function = borrowed.TransientFunction
	}
	host["subtleImportRSAOAEP"] = function(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		der := byteSlice(arg(a, 0))
		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, fmt.Errorf("invalid SPKI key: %w", err)
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("SPKI key is not RSA")
		}
		exponent := key.E
		publicExponent := []int{}
		for shift := 24; shift >= 0; shift -= 8 {
			value := (exponent >> shift) & 0xff
			if value != 0 || len(publicExponent) != 0 {
				publicExponent = append(publicExponent, value)
			}
		}
		parts := make([]string, len(publicExponent))
		for i, value := range publicExponent {
			parts[i] = strconv.Itoa(value)
		}
		return runtime.Value(strconv.Itoa(key.N.BitLen()) + "|" + strings.Join(parts, ",")), nil
	})
	host["subtleRSAOAEPEncrypt"] = function(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		algorithm, der, input, label := strings.ToUpper(strings.ReplaceAll(strarg(a, 0), "_", "-")), byteSlice(arg(a, 1)), byteSlice(arg(a, 2)), byteSlice(arg(a, 3))
		parsed, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, fmt.Errorf("invalid SPKI key: %w", err)
		}
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("SPKI key is not RSA")
		}
		var hash crypto.Hash
		switch algorithm {
		case "SHA-1":
			hash = crypto.SHA1
		case "SHA-256":
			hash = crypto.SHA256
		case "SHA-384":
			hash = crypto.SHA384
		case "SHA-512":
			hash = crypto.SHA512
		default:
			return nil, fmt.Errorf("unsupported RSA-OAEP hash %q", algorithm)
		}
		ciphertext, err := rsa.EncryptOAEP(hash.New(), rand.Reader, key, input, label)
		if err != nil {
			return nil, err
		}
		out := make([]int, len(ciphertext))
		for i, value := range ciphertext {
			out[i] = int(value)
		}
		return runtime.Value(out), nil
	})
	host["webCrypto"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		var request webCryptoRequest
		if err := json.Unmarshal([]byte(strarg(args, 0)), &request); err != nil {
			return nil, err
		}
		output, err := executeWebCrypto(request)
		result := map[string]any{}
		if err != nil {
			result["error"] = err.Error()
		} else if verified, ok := output.(bool); ok {
			result["verified"] = verified
		} else {
			bytes := output.([]byte)
			values := make([]int, len(bytes))
			for i, b := range bytes {
				values[i] = int(b)
			}
			result["bytes"] = values
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return runtime.Value(string(encoded)), nil
	})
}

func executeWebCrypto(r webCryptoRequest) (any, error) {
	if r.Operation == "random" {
		if r.Length < 1 || r.Length > 1<<20 {
			return nil, errors.New("invalid random key length")
		}
		out := make([]byte, r.Length)
		_, err := rand.Read(out)
		return out, err
	}
	if r.Operation == "digest" {
		hash, err := webCryptoHash(r.Hash)
		if err != nil {
			return nil, err
		}
		h := hash.New()
		h.Write(r.Data)
		return h.Sum(nil), nil
	}
	if r.Name == "HMAC" {
		hash, err := webCryptoHash(r.Hash)
		if err != nil {
			return nil, err
		}
		h := hmac.New(hash.New, r.Key)
		h.Write(r.Data)
		out := h.Sum(nil)
		if r.Operation == "verify" {
			return hmac.Equal(out, r.Signature), nil
		}
		return out, nil
	}
	if r.Name == "PBKDF2" || r.Name == "HKDF" {
		hash, err := webCryptoHash(r.Hash)
		if err != nil {
			return nil, err
		}
		if r.Length < 0 || r.Length > 1<<24 {
			return nil, errors.New("invalid derived key length")
		}
		if r.Name == "PBKDF2" {
			if r.Iterations < 1 {
				return nil, errors.New("invalid iteration count")
			}
			return pbkdf2.Key(r.Key, r.Salt, r.Iterations, r.Length, hash.New), nil
		}
		out := make([]byte, r.Length)
		_, err = io.ReadFull(hkdf.New(hash.New, r.Key, r.Salt, r.Info), out)
		return out, err
	}
	block, err := aes.NewCipher(r.Key)
	if err != nil {
		return nil, err
	}
	switch r.Name {
	case "AES-KW":
		if r.Operation == "encrypt" {
			if len(r.Data) < 16 || len(r.Data)%8 != 0 {
				return nil, errors.New("invalid AES-KW plaintext")
			}
			out := make([]byte, len(r.Data)+8)
			for i := 0; i < 8; i++ {
				out[i] = 0xa6
			}
			copy(out[8:], r.Data)
			n := len(r.Data) / 8
			buffer := make([]byte, 16)
			for j := 0; j < 6; j++ {
				for i := 1; i <= n; i++ {
					copy(buffer, out[:8])
					copy(buffer[8:], out[8*i:8*i+8])
					block.Encrypt(buffer, buffer)
					binary.BigEndian.PutUint64(out[:8], binary.BigEndian.Uint64(buffer[:8])^uint64(n*j+i))
					copy(out[8*i:8*i+8], buffer[8:])
				}
			}
			return out, nil
		}
		if len(r.Data) < 24 || len(r.Data)%8 != 0 {
			return nil, errors.New("invalid AES-KW ciphertext")
		}
		out := append([]byte(nil), r.Data...)
		n := len(out)/8 - 1
		buffer := make([]byte, 16)
		for j := 5; j >= 0; j-- {
			for i := n; i >= 1; i-- {
				binary.BigEndian.PutUint64(buffer[:8], binary.BigEndian.Uint64(out[:8])^uint64(n*j+i))
				copy(buffer[8:], out[8*i:8*i+8])
				block.Decrypt(buffer, buffer)
				copy(out[:8], buffer[:8])
				copy(out[8*i:8*i+8], buffer[8:])
			}
		}
		if subtle.ConstantTimeCompare(out[:8], []byte{0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6}) != 1 {
			return nil, errors.New("AES-KW authentication failed")
		}
		return out[8:], nil
	case "AES-GCM":
		if len(r.IV) == 0 {
			return nil, errors.New("empty GCM nonce")
		}
		aead, err := cipher.NewGCMWithNonceSize(block, len(r.IV))
		if err != nil {
			return nil, err
		}
		tagBytes := r.TagLength / 8
		if tagBytes != 4 && tagBytes != 8 && (tagBytes < 12 || tagBytes > 16) {
			return nil, errors.New("invalid GCM tag length")
		}
		if r.Operation == "encrypt" {
			out := aead.Seal(nil, r.IV, r.Data, r.AdditionalData)
			return out[:len(out)-16+tagBytes], nil
		}
		if len(r.Data) < tagBytes {
			return nil, errors.New("truncated GCM ciphertext")
		}
		ciphertext := r.Data[:len(r.Data)-tagBytes]
		// The standard library supplies nonce processing, counter mode and GHASH.
		// For WebCrypto's short tags, recover candidate bytes using its keystream,
		// then authenticate the requested tag in constant time before releasing any.
		stream := aead.Seal(nil, r.IV, make([]byte, len(ciphertext)), nil)
		plaintext := make([]byte, len(ciphertext))
		for i, b := range ciphertext {
			plaintext[i] = b ^ stream[i]
		}
		authenticated := aead.Seal(nil, r.IV, plaintext, r.AdditionalData)
		if subtle.ConstantTimeCompare(authenticated[len(ciphertext):len(ciphertext)+tagBytes], r.Data[len(ciphertext):]) != 1 {
			return nil, errors.New("GCM authentication failed")
		}
		return plaintext, nil
	case "AES-CBC":
		if len(r.IV) != 16 {
			return nil, errors.New("invalid CBC IV")
		}
		if r.Operation == "encrypt" {
			padding := 16 - len(r.Data)%16
			out := make([]byte, len(r.Data)+padding)
			copy(out, r.Data)
			for i := len(r.Data); i < len(out); i++ {
				out[i] = byte(padding)
			}
			cipher.NewCBCEncrypter(block, r.IV).CryptBlocks(out, out)
			return out, nil
		}
		if len(r.Data) == 0 || len(r.Data)%16 != 0 {
			return nil, errors.New("invalid CBC ciphertext")
		}
		out := make([]byte, len(r.Data))
		cipher.NewCBCDecrypter(block, r.IV).CryptBlocks(out, r.Data)
		padding := int(out[len(out)-1])
		valid := subtle.ConstantTimeLessOrEq(1, padding) & subtle.ConstantTimeLessOrEq(padding, 16)
		for i := 1; i <= 16; i++ {
			active := subtle.ConstantTimeLessOrEq(i, padding)
			valid &= 1 - active | (subtle.ConstantTimeByteEq(out[len(out)-i], byte(padding)) & active)
		}
		if valid != 1 {
			return nil, errors.New("invalid CBC padding")
		}
		return out[:len(out)-padding], nil
	case "AES-CTR":
		if len(r.IV) != 16 || r.Length < 1 || r.Length > 128 {
			return nil, errors.New("invalid CTR parameters")
		}
		blocks := (len(r.Data) + 15) / 16
		if r.Length < 63 && uint64(blocks) > uint64(1)<<r.Length {
			return nil, errors.New("CTR counter exhausted")
		}
		counter := append([]byte(nil), r.IV...)
		out := make([]byte, len(r.Data))
		stream := make([]byte, 16)
		for offset := 0; offset < len(out); offset += 16 {
			block.Encrypt(stream, counter)
			for i := 0; i < 16 && offset+i < len(out); i++ {
				out[offset+i] = r.Data[offset+i] ^ stream[i]
			}
			for bit := 0; bit < r.Length; bit++ {
				index := 15 - bit/8
				mask := byte(1 << uint(bit%8))
				counter[index] ^= mask
				if counter[index]&mask != 0 {
					break
				}
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported algorithm %q", r.Name)
}
