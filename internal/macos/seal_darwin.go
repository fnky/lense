//go:build darwin

package macos

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Sealed is a secret that a key of the Secure Enclave encrypts, which
// Touch ID alone lets decrypt: the key never leaves the chip, nor is it
// kept anywhere, so the secret opens in this process alone, until it
// quits or Release. A dump of the process's memory holds it encrypted.
type Sealed struct {
	priv, access uintptr // SecKeyRef, SecAccessControlRef
	ctx          objc.ID // the LAContext the key uses, which Open authorizes
	data         uintptr // CFDataRef, the secret encrypted
}

var (
	secOnce sync.Once
	secErr  error

	secAccessCreate func(alloc, protection uintptr, flags uint, err *uintptr) uintptr
	secRandomKey    func(params uintptr, err *uintptr) uintptr
	secCopyPublic   func(key uintptr) uintptr
	secEncrypt      func(key, alg, data uintptr, err *uintptr) uintptr
	secDecrypt      func(key, alg, data uintptr, err *uintptr) uintptr

	cfDictCreate  func(alloc uintptr, capacity int, keys, values uintptr) uintptr
	cfDictSet     func(dict, key, value uintptr)
	cfNumber      func(alloc uintptr, typ int, value unsafe.Pointer) uintptr
	cfDataNoCopy  func(alloc uintptr, b *byte, n int, dealloc uintptr) uintptr
	cfDataLen     func(data uintptr) int
	cfDataPtr     func(data uintptr) *byte
	cfErrCode     func(err uintptr) int
	cfReleaseRefs func(ref uintptr)

	// Constants of Security and CoreFoundation, the values they point to.
	kPermanent, kAccessControl, kKeyType, kKeyTypeEC, kKeySize uintptr
	kTokenID, kTokenSecureEnclave, kPrivateAttrs, kAuthContext uintptr
	kWhenUnlockedThisDevice, kECIES, kFalse, kAllocatorNull    uintptr
	kDictKeyCallBacks, kDictValueCallBacks                     uintptr
)

// The flags of the key's access control: Touch ID, with the fingerprints
// enrolled as the secret was sealed, to use the private key. The Secure
// Enclave decrypts ECIES by a key exchange, the one use of the key its
// access control lets Touch ID allow: allowing a decryption fails, at
// once, as an operation not allowed.
const (
	accessBiometryCurrentSet = 1 << 3
	accessPrivateKeyUsage    = 1 << 30
	laUseKeyKeyExchange      = 5 // LAAccessControlOperationUseKeyKeyExchange
)

func loadSecurity() error {
	secOnce.Do(func() {
		if secErr = load(); secErr != nil {
			return
		}
		sec, err := lib("/System/Library/Frameworks/Security.framework/Security")
		if err != nil {
			secErr = err
			return
		}
		cf, err := lib("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		if err != nil {
			secErr = err
			return
		}
		purego.RegisterLibFunc(&secAccessCreate, sec, "SecAccessControlCreateWithFlags")
		purego.RegisterLibFunc(&secRandomKey, sec, "SecKeyCreateRandomKey")
		purego.RegisterLibFunc(&secCopyPublic, sec, "SecKeyCopyPublicKey")
		purego.RegisterLibFunc(&secEncrypt, sec, "SecKeyCreateEncryptedData")
		purego.RegisterLibFunc(&secDecrypt, sec, "SecKeyCreateDecryptedData")
		purego.RegisterLibFunc(&cfDictCreate, cf, "CFDictionaryCreateMutable")
		purego.RegisterLibFunc(&cfDictSet, cf, "CFDictionarySetValue")
		purego.RegisterLibFunc(&cfNumber, cf, "CFNumberCreate")
		purego.RegisterLibFunc(&cfDataNoCopy, cf, "CFDataCreateWithBytesNoCopy")
		purego.RegisterLibFunc(&cfDataLen, cf, "CFDataGetLength")
		purego.RegisterLibFunc(&cfDataPtr, cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&cfErrCode, cf, "CFErrorGetCode")
		purego.RegisterLibFunc(&cfReleaseRefs, cf, "CFRelease")
		value := func(lib uintptr, name string) uintptr {
			p, err := purego.Dlsym(lib, name)
			if err != nil {
				secErr = err
				return 0
			}
			return *(*uintptr)(cptr(p))
		}
		address := func(lib uintptr, name string) uintptr {
			p, err := purego.Dlsym(lib, name)
			if err != nil {
				secErr = err
			}
			return p
		}
		kPermanent = value(sec, "kSecAttrIsPermanent")
		kAccessControl = value(sec, "kSecAttrAccessControl")
		kKeyType = value(sec, "kSecAttrKeyType")
		kKeyTypeEC = value(sec, "kSecAttrKeyTypeECSECPrimeRandom")
		kKeySize = value(sec, "kSecAttrKeySizeInBits")
		kTokenID = value(sec, "kSecAttrTokenID")
		kTokenSecureEnclave = value(sec, "kSecAttrTokenIDSecureEnclave")
		kPrivateAttrs = value(sec, "kSecPrivateKeyAttrs")
		kAuthContext = value(sec, "kSecUseAuthenticationContext")
		kWhenUnlockedThisDevice = value(sec, "kSecAttrAccessibleWhenUnlockedThisDeviceOnly")
		kECIES = value(sec, "kSecKeyAlgorithmECIESEncryptionCofactorVariableIVX963SHA256AESGCM")
		kFalse = value(cf, "kCFBooleanFalse")
		kAllocatorNull = value(cf, "kCFAllocatorNull")
		kDictKeyCallBacks = address(cf, "kCFTypeDictionaryKeyCallBacks")
		kDictValueCallBacks = address(cf, "kCFTypeDictionaryValueCallBacks")
	})
	return secErr
}

// Seal encrypts secret with a new key of the Secure Enclave, which only
// Touch ID lets decrypt, with the fingerprints enrolled now. secret is
// left as it was: wipe it. Seal asks for nothing.
func Seal(secret []byte) (*Sealed, error) {
	if len(secret) == 0 {
		return nil, errors.New("macos: nothing to seal")
	}
	if err := loadSecurity(); err != nil {
		return nil, ErrBiometricsUnavailable
	}
	var s *Sealed
	err := ErrBiometricsUnavailable
	withPool(func() {
		var cfErr uintptr
		access := secAccessCreate(0, kWhenUnlockedThisDevice, accessBiometryCurrentSet|accessPrivateKeyUsage, &cfErr)
		if access == 0 {
			releaseRef(cfErr)
			return
		}
		ctx := class("LAContext").Send(sel("alloc")).Send(sel("init"))
		privAttrs := cfDictCreate(0, 0, kDictKeyCallBacks, kDictValueCallBacks)
		cfDictSet(privAttrs, kPermanent, kFalse)
		cfDictSet(privAttrs, kAccessControl, access)
		cfDictSet(privAttrs, kAuthContext, uintptr(ctx))
		params := cfDictCreate(0, 0, kDictKeyCallBacks, kDictValueCallBacks)
		bits := int32(256)
		size := cfNumber(0, 3, unsafe.Pointer(&bits)) // kCFNumberSInt32Type
		cfDictSet(params, kKeyType, kKeyTypeEC)
		cfDictSet(params, kKeySize, size)
		cfDictSet(params, kTokenID, kTokenSecureEnclave)
		cfDictSet(params, kPrivateAttrs, privAttrs)
		priv := secRandomKey(params, &cfErr)
		releaseRef(size)
		releaseRef(params)
		releaseRef(privAttrs)
		if priv == 0 {
			releaseRef(cfErr)
			releaseRef(access)
			ctx.Send(sel("release"))
			return
		}
		pub := secCopyPublic(priv)
		// The secret stays in Go's memory, which the caller wipes: no copy
		// of it for CoreFoundation to free unwiped.
		plain := cfDataNoCopy(0, &secret[0], len(secret), kAllocatorNull)
		data := secEncrypt(pub, kECIES, plain, &cfErr)
		runtime.KeepAlive(secret)
		releaseRef(plain)
		releaseRef(pub)
		if data == 0 {
			releaseRef(cfErr)
			releaseRef(priv)
			releaseRef(access)
			ctx.Send(sel("release"))
			err = errors.New("macos: the Secure Enclave did not seal the secret")
			return
		}
		s, err = &Sealed{priv: priv, access: access, ctx: ctx, data: data}, nil
	})
	return s, err
}

// Open asks for Touch ID, saying why, and decrypts the secret: wipe it
// once used. Like Authenticate, it returns ErrCanceled when the user
// canceled, ErrFallback when they chose fallback, and ErrSealedGone when
// the secret can't be opened any more.
func (s *Sealed) Open(reason, fallback string) ([]byte, error) {
	if s == nil || s.priv == 0 {
		return nil, ErrSealedGone
	}
	if err := s.authorize(reason, fallback); err != nil {
		return nil, err
	}
	var out []byte
	var err error
	withPool(func() {
		var cfErr uintptr
		plain := secDecrypt(s.priv, kECIES, s.data, &cfErr)
		if plain == 0 {
			releaseRef(cfErr)
			err = ErrSealedGone
			return
		}
		n := cfDataLen(plain)
		b := unsafe.Slice(cfDataPtr(plain), n)
		out = make([]byte, n)
		copy(out, b)
		clear(b) // CoreFoundation's copy, before it frees it
		releaseRef(plain)
	})
	return out, err
}

// authorize asks for Touch ID, which lets the key's context decrypt the
// secret, without asking again.
func (s *Sealed) authorize(reason, fallback string) error {
	done := make(chan error, 1)
	var block objc.Block
	withPool(func() {
		s.ctx.Send(sel("setLocalizedFallbackTitle:"), nsString(fallback))
		block = objc.NewBlock(func(_ objc.Block, success bool, nsErr objc.ID) {
			if success {
				done <- nil
				return
			}
			done <- laError(nsErr)
		})
		s.ctx.Send(sel("evaluateAccessControl:operation:localizedReason:reply:"), s.access, laUseKeyKeyExchange, nsString(reason), block)
	})
	err := <-done
	block.Release()
	return err
}

// Release forgets the key, which nothing can open the secret with then.
func (s *Sealed) Release() {
	if s == nil || s.priv == 0 {
		return
	}
	withPool(func() {
		releaseRef(s.data)
		releaseRef(s.priv)
		releaseRef(s.access)
		s.ctx.Send(sel("invalidate"))
		s.ctx.Send(sel("release"))
	})
	*s = Sealed{}
}

func releaseRef(ref uintptr) {
	if ref != 0 {
		cfReleaseRefs(ref)
	}
}
