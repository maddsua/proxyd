package rpc

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type InstanceToken struct {
	ID        uuid.UUID
	SecretKey RawSecretKey
}

func (token *InstanceToken) String() string {

	if token == nil {
		return "<nil>"
	}

	return fmt.Sprintf("%s.%s",
		base64.RawURLEncoding.EncodeToString(token.ID[:]),
		base64.RawURLEncoding.EncodeToString(token.SecretKey.Bytes))
}

func (token *InstanceToken) UnmarshalText(text string) error {

	if text == "" {
		return fmt.Errorf("empty token value")
	}

	before, after, has := strings.Cut(text, ".")
	if !has {
		return fmt.Errorf("illformed token value")
	}

	var decodeBase = func(val string) ([]byte, error) {
		return base64.RawURLEncoding.DecodeString(val)
	}

	var decodeBaseUUID = func(val string) (uuid.UUID, error) {
		bytes, err := decodeBase(val)
		if err != nil {
			return uuid.UUID{}, err
		}
		return uuid.FromBytes(bytes)
	}

	if id, err := decodeBaseUUID(before); err != nil {
		return fmt.Errorf("illformed token ID: %v", err)
	} else {
		token.ID = id
	}

	if secret, err := decodeBase(after); err != nil {
		return fmt.Errorf("illformed token key")
	} else if len(secret) == 0 {
		return fmt.Errorf("empty token key")
	} else {
		token.SecretKey.Bytes = secret
	}

	return nil
}

func (token *InstanceToken) UnmarshalYAML(value *yaml.Node) error {
	return token.UnmarshalText(value.Value)
}

func (token *InstanceToken) MarshalText() ([]byte, error) {
	if token == nil {
		return nil, nil
	}

	return []byte(token.String()), nil
}

func ParseInstanceToken(text string) (*InstanceToken, error) {

	var token InstanceToken
	if err := token.UnmarshalText(text); err != nil {
		return nil, err
	}

	return &token, nil
}

func NewInstanceToken() (*InstanceToken, error) {

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("generate ID: %v", err)
	}

	secret := make([]byte, 64)
	if _, err := rand.Reader.Read(secret); err != nil {
		panic("read random: " + err.Error())
	}

	return &InstanceToken{ID: id, SecretKey: RawSecretKey{Bytes: secret}}, nil
}

type RawSecretKey struct {
	Bytes []byte
}

func (secret *RawSecretKey) Equal(other *RawSecretKey) bool {
	if secret == nil || other == nil {
		return false
	}
	return subtle.ConstantTimeCompare(secret.Bytes, other.Bytes) == 1
}

func (secret *RawSecretKey) String() string {
	return base64.RawURLEncoding.EncodeToString(secret.Bytes)
}

func (secret *RawSecretKey) UnmarshalText(value string) (err error) {
	secret.Bytes, err = base64.RawURLEncoding.DecodeString(value)
	return
}

func (secret *RawSecretKey) UnmarshalYAML(value *yaml.Node) error {
	return secret.UnmarshalText(value.Value)
}

func (secret *RawSecretKey) UnmarshalJSON(data []byte) (err error) {

	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}

	return secret.UnmarshalText(text)
}
