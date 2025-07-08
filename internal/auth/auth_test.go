package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	hashed, err := HashPassword("password")
	if err != nil {
		t.Error(err)
	}

	checkErr := bcrypt.CompareHashAndPassword([]byte(hashed), []byte("password"))
	if checkErr != nil {
		t.Error(err)
	}
}

func TestCheckPassword(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		t.Error(err)
	}

	checkErr := CheckPasswordHash("password", string(hashed))
	if checkErr != nil {
		t.Error(err)
	}
}

func TestCheckPasswordBlank(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	if err != nil {
		t.Error(err)
	}

	checkErr := CheckPasswordHash("", string(hashed))
	if checkErr == nil {
		t.Error("Expected error, got nil")
	}
}

func TestMakeJWT(t *testing.T) {
	token, err := MakeJWT(uuid.MustParse("36feb268-98ca-4300-8fdd-a96bade43beb"),
		"LeyPhlefurapwopEitKo", time.Duration(1000*1000*1000*1000))
	if err != nil {
		t.Fatalf("Error creating JWT: %v", err)
	}
	if token == "" {
		t.Error("Expected JWT token, got empty string")
	}

	extractedToken := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(token, extractedToken, func(token *jwt.Token) (interface{}, error) {
		return []byte("LeyPhlefurapwopEitKo"), nil
	})
	if err != nil {
		t.Fatalf("Error decoding created JWT: %v", err)
	}
}

func TestValidateJWT(t *testing.T) {
	userID := uuid.MustParse("36feb268-98ca-4300-8fdd-a96bade43beb")
	secret := "LeyPhlefurapwopEitKo"
	token, err := MakeJWT(userID, secret, time.Duration(1000*1000*1000*1000))
	if err != nil {
		t.Fatalf("Error creating JWT: %v", err)
	}

	// Test valid token
	extractedID, err := ValidateJWT(token, secret)
	if err != nil {
		t.Fatalf("Error validating JWT: %v", err)
	}
	if extractedID != userID {
		t.Errorf("Expected user ID %v, got %v", userID, extractedID)
	}

	// Test invalid token
	_, err = ValidateJWT("invalid.token.string", secret)
	if err == nil {
		t.Error("Expected error for invalid token, got nil")
	}

	// Test expired token
	expiredToken, _ := MakeJWT(userID, secret, -time.Hour)
	_, err = ValidateJWT(expiredToken, secret)
	if err == nil {
		t.Error("Expected error for expired token, got nil")
	}
}
