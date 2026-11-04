package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidate_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth secrettoken" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_id":"abc","login":"pedrosatin","scopes":["chat:read","chat:edit"],"user_id":"1","expires_in":3600}`))
	}))
	defer srv.Close()

	v, err := (ValidateClient{URL: srv.URL, HTTPClient: srv.Client()}).Validate(context.Background(), "secrettoken")
	if err != nil {
		t.Fatal(err)
	}
	if v.Login != "pedrosatin" {
		t.Fatalf("login = %q", v.Login)
	}
	if len(v.Scopes) != 2 {
		t.Fatalf("scopes = %#v", v.Scopes)
	}
	if v.ExpiresIn != 3600 {
		t.Fatalf("expires = %d", v.ExpiresIn)
	}
}

func TestValidate_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"message":"invalid access token"}`))
	}))
	defer srv.Close()

	_, err := (ValidateClient{URL: srv.URL, HTTPClient: srv.Client()}).Validate(context.Background(), "bad")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("err = %v, want ErrTokenInvalid", err)
	}
	if err != nil && strings.Contains(err.Error(), "bad") {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestValidate_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"x"}`))
	}))
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 20 * time.Millisecond
	_, err := (ValidateClient{URL: srv.URL, HTTPClient: client}).Validate(context.Background(), "tok")
	if !errors.Is(err, ErrValidateTimeout) {
		t.Fatalf("err = %v, want ErrValidateTimeout", err)
	}
}
