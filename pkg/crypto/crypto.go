package crypto

import (
	"crypto/des"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

const DesBlockSize = 8

// MD5 returns the 32-character uppercase hex MD5 checksum of data.
func MD5(data string) string {
	sum := md5.Sum([]byte(data))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// DESEncrypt encrypts sText using DES-ECB with a key derived from MD5(strKey).
// Each 8-byte chunk is encrypted and formatted as 16 uppercase hex characters.
func DESEncrypt(strKey, sText string) string {
	k := MD5(strKey)
	keyBytes := make([]byte, DesBlockSize)
	copy(keyBytes, []byte(k[:min(len(k), DesBlockSize)]))

	cipher, err := des.NewCipher(keyBytes)
	if err != nil {
		return ""
	}

	rawText := []byte(sText)
	var sb strings.Builder

	for i := 0; i < len(rawText); i += DesBlockSize {
		chunk := make([]byte, DesBlockSize)
		end := min(i+DesBlockSize, len(rawText))
		copy(chunk, rawText[i:end])

		dst := make([]byte, DesBlockSize)
		cipher.Encrypt(dst, chunk)

		sb.WriteString(strings.ToUpper(hex.EncodeToString(dst)))
	}

	return sb.String()
}

// DESDecrypt decrypts sHex using DES-ECB with a key derived from MD5(strKey).
// Every 16 hex characters represent an 8-byte block.
// Null bytes in the decrypted block (padding) are stripped at the first null character per chunk,
// matching the C++ std::string constructor behavior.
func DESDecrypt(strKey, sHex string) string {
	k := MD5(strKey)
	keyBytes := make([]byte, DesBlockSize)
	copy(keyBytes, []byte(k[:min(len(k), DesBlockSize)]))

	cipher, err := des.NewCipher(keyBytes)
	if err != nil {
		return ""
	}

	sHex = strings.TrimSpace(sHex)
	var sb strings.Builder

	chunkHexLen := DesBlockSize * 2
	for i := 0; i < len(sHex); i += chunkHexLen {
		end := min(i+chunkHexLen, len(sHex))
		blockHex := sHex[i:end]
		if len(blockHex) < chunkHexLen {
			break
		}

		cipherBlock, err := hex.DecodeString(blockHex)
		if err != nil || len(cipherBlock) != DesBlockSize {
			continue
		}

		plainBlock := make([]byte, DesBlockSize)
		cipher.Decrypt(plainBlock, cipherBlock)

		// Find first null byte if any (C++ std::string(const char*) behavior)
		nullIdx := len(plainBlock)
		for idx, b := range plainBlock {
			if b == 0 {
				nullIdx = idx
				break
			}
		}

		sb.Write(plainBlock[:nullIdx])
	}

	return sb.String()
}

// GetMACAddress returns the MAC address of the first active non-loopback network interface,
// or a pseudo-random MAC if none is available.
func GetMACAddress() string {
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagLoopback == 0 && len(iface.HardwareAddr) > 0 {
				return iface.HardwareAddr.String()
			}
		}
	}
	return "00:0c:29:11:22:33"
}

// NewGUID generates a unique GUID based on random number, timestamps, MAC address, and param.
func NewGUID(param string) string {
	r, _ := rand.Int(rand.Reader, big.NewInt(1000000000))
	now := time.Now()
	sec := now.Unix()
	ms := now.UnixMilli()
	mac := GetMACAddress()

	seed := fmt.Sprintf("%d_%d_%d_%s_%s", r.Int64(), sec, ms, mac, param)
	return MD5(seed)
}
