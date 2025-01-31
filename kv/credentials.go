package kv

import "fmt"

const CredentialsPrefix = "mg_creds"

type Credential struct {
	APIKey string `json:"api_key"`
}

type CredentialStore struct {
	store *Store
}

func credentialsStringConcat(credentialString string) string {
	return fmt.Sprintf("mg_creds:%s", credentialString)
}

func NewCredentialStore(store *Store) *CredentialStore {
	return &CredentialStore{store: store}
}

func (cd *CredentialStore) CreateCredential(platform string, value string) error {
	return cd.store.Set(platform, value)
}

func (cd *CredentialStore) GetCredential(platform string) (string, *error) {
	// err := cd.store.Get(platform, )
}
