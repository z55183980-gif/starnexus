package common

import (
	"regexp"
	"testing"
)

func TestNumericVerificationCodeAndAttemptLimit(t *testing.T) {
	origRedis := RedisEnabled
	RedisEnabled = false
	t.Cleanup(func() {
		RedisEnabled = origRedis
	})

	digitsOnly := regexp.MustCompile(`^[0-9]{6}$`)
	for i := 0; i < 50; i++ {
		code := GenerateNumericVerificationCode(6)
		if !digitsOnly.MatchString(code) {
			t.Fatalf("expected 6 numeric digits, got %q", code)
		}
	}

	email := "TestUser@Example.com"
	RegisterVerificationCodeWithKey(email, "482915", EmailVerificationPurpose)

	// Case-insensitive email key and whitespace-trimmed code should verify without consuming prematurely
	if !VerifyCodeWithKey("testuser@example.com", " 482915 ", EmailVerificationPurpose) {
		t.Fatal("expected valid code to match case-insensitively")
	}

	// Explicit DeleteKey after registration consumes the code
	DeleteKey("testuser@example.com", EmailVerificationPurpose)
	if VerifyCodeWithKey(email, "482915", EmailVerificationPurpose) {
		t.Fatal("expected code to be invalid after DeleteKey")
	}

	// Brute-force protection: MaxVerificationAttempts wrong guesses invalidate the code
	RegisterVerificationCodeWithKey(email, "918273", EmailVerificationPurpose)
	for i := 0; i < MaxVerificationAttempts; i++ {
		if VerifyCodeWithKey(email, "000000", EmailVerificationPurpose) {
			t.Fatal("unexpected match on wrong code")
		}
	}
	if VerifyCodeWithKey(email, "918273", EmailVerificationPurpose) {
		t.Fatalf("expected code to be invalidated after %d failed attempts", MaxVerificationAttempts)
	}
}
