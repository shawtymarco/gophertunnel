package packet

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Encryption encrypts, decrypts and verifies Minecraft packet batches. Implementations are stateful and must not be
// shared between the encoder and decoder of a connection.
type Encryption interface {
	Encrypt(data []byte) []byte
	Decrypt(data []byte)
	Verify(data []byte) error
}

// encrypt holds an encryption session with several fields required to encrypt and/or decrypt incoming
// packets. It may be initialised using secret key bytes computed using the shared secret produced with a
// private and a public ECDSA key.
type encrypt struct {
	sendCounter uint64
	buf         [8]byte
	keyBytes    []byte
	stream      cipher.Stream
}

// newEncrypt returns a new encryption 'session' using the secret key bytes passed. The session has its cipher
// block and IV prepared so that it may be used to decrypt and encrypt data.
func newEncrypt(keyBytes []byte, stream cipher.Stream) *encrypt {
	return &encrypt{keyBytes: append([]byte(nil), keyBytes...), stream: stream}
}

// NewCTREncryption returns the encryption used by current Minecraft versions.
func NewCTREncryption(keyBytes [32]byte) Encryption {
	block, _ := aes.NewCipher(keyBytes[:])
	first12 := append([]byte(nil), keyBytes[:12]...)
	return newEncrypt(keyBytes[:], cipher.NewCTR(block, append(first12, 0, 0, 0, 2)))
}

// NewCFB8Encryption returns the legacy CFB8 encryption used by Minecraft versions before 1.16.220.
func NewCFB8Encryption(keyBytes [32]byte) Encryption {
	block, _ := aes.NewCipher(keyBytes[:])
	return &cfb8Encryption{
		keyBytes: append([]byte(nil), keyBytes[:]...),
		encrypt:  newCFB8(block, keyBytes[:block.BlockSize()], false),
		decrypt:  newCFB8(block, keyBytes[:block.BlockSize()], true),
	}
}

// Encrypt encrypts the data passed, adding the packet checksum at the end before applying the configured stream.
func (encrypt *encrypt) Encrypt(data []byte) []byte {
	// We first write the current send counter to a buffer and use it to produce a packet checksum.
	binary.LittleEndian.PutUint64(encrypt.buf[:], encrypt.sendCounter)
	encrypt.sendCounter++

	// We produce a hash existing of the send counter, packet data and key bytes.
	hash := sha256.New()
	hash.Write(encrypt.buf[:])
	hash.Write(data[1:])
	hash.Write(encrypt.keyBytes)

	// We add the first 8 bytes of the checksum to the data and encrypt it.
	data = append(data, hash.Sum(nil)[:8]...)

	encrypt.stream.XORKeyStream(data[1:], data[1:])
	return data
}

// Decrypt decrypts the data passed. It does not verify the packet checksum. Verify must be called afterwards.
func (encrypt *encrypt) Decrypt(data []byte) {
	encrypt.stream.XORKeyStream(data, data)
}

// Verify verifies the packet checksum of the decrypted data passed. If successful, nil is returned. Otherwise
// an error is returned describing the invalid checksum.
func (encrypt *encrypt) Verify(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("encrypted packet must be at least 8 bytes long, got %v", len(data))
	}
	sum := data[len(data)-8:]

	// We first write the current send counter to a buffer and use it to produce a packet checksum.
	binary.LittleEndian.PutUint64(encrypt.buf[:], encrypt.sendCounter)
	encrypt.sendCounter++

	// We produce a hash existing of the send counter, packet data and key bytes.
	hash := sha256.New()
	hash.Write(encrypt.buf[:])
	hash.Write(data[:len(data)-8])
	hash.Write(encrypt.keyBytes)
	ourSum := hash.Sum(nil)[:8]

	// Finally we check if the original sum was equal to the sum we just produced.
	if !bytes.Equal(sum, ourSum) {
		return fmt.Errorf("invalid checksum of packet %v: expected %x, got %x", encrypt.sendCounter-1, ourSum, sum)
	}
	return nil
}

type cfb8Encryption struct {
	sendCounter uint64
	buf         [8]byte
	keyBytes    []byte
	encrypt     cipher.Stream
	decrypt     cipher.Stream
}

func (encryption *cfb8Encryption) Encrypt(data []byte) []byte {
	binary.LittleEndian.PutUint64(encryption.buf[:], encryption.sendCounter)
	encryption.sendCounter++
	hash := sha256.New()
	hash.Write(encryption.buf[:])
	hash.Write(data[1:])
	hash.Write(encryption.keyBytes)
	data = append(data, hash.Sum(nil)[:8]...)
	encryption.encrypt.XORKeyStream(data[1:], data[1:])
	return data
}

func (encryption *cfb8Encryption) Decrypt(data []byte) {
	encryption.decrypt.XORKeyStream(data, data)
}

func (encryption *cfb8Encryption) Verify(data []byte) error {
	if len(data) < 8 {
		return fmt.Errorf("encrypted packet must be at least 8 bytes long, got %v", len(data))
	}
	sum := data[len(data)-8:]
	binary.LittleEndian.PutUint64(encryption.buf[:], encryption.sendCounter)
	encryption.sendCounter++
	hash := sha256.New()
	hash.Write(encryption.buf[:])
	hash.Write(data[:len(data)-8])
	hash.Write(encryption.keyBytes)
	ourSum := hash.Sum(nil)[:8]
	if !bytes.Equal(sum, ourSum) {
		return fmt.Errorf("invalid checksum of packet %v: expected %x, got %x", encryption.sendCounter-1, ourSum, sum)
	}
	return nil
}

// cfb8 implements cipher feedback mode with an 8-bit segment size.
type cfb8 struct {
	block     cipher.Block
	blockSize int
	in        []byte
	out       []byte
	decrypt   bool
}

func newCFB8(block cipher.Block, iv []byte, decrypt bool) cipher.Stream {
	if len(iv) != block.BlockSize() {
		panic("packet: CFB8 IV length must equal block size")
	}
	stream := &cfb8{
		block:     block,
		blockSize: block.BlockSize(),
		in:        make([]byte, block.BlockSize()),
		out:       make([]byte, block.BlockSize()),
		decrypt:   decrypt,
	}
	copy(stream.in, iv)
	return stream
}

func (stream *cfb8) XORKeyStream(dst, src []byte) {
	if len(dst) < len(src) {
		panic("packet: output smaller than input")
	}
	for i := range src {
		stream.block.Encrypt(stream.out, stream.in)
		copy(stream.in[:stream.blockSize-1], stream.in[1:])
		if stream.decrypt {
			stream.in[stream.blockSize-1] = src[i]
		}
		dst[i] = src[i] ^ stream.out[0]
		if !stream.decrypt {
			stream.in[stream.blockSize-1] = dst[i]
		}
	}
}
