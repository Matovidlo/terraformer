// Copyright 2020 The Terraformer Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gmailfilter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/terraformer/terraformutils"
	"github.com/mitchellh/go-homedir"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

const gmailUser = "me"

var gmailAPIScopes = []string{
	gmail.GmailLabelsScope,
	gmail.GmailSettingsBasicScope,
}

type GmailfilterService struct { //nolint
	terraformutils.Service
}

func (s *GmailfilterService) gmailService(ctx context.Context) (*gmail.Service, error) {
	credsPathOrJSON := s.GetArgs()["credentials"].(string)
	impersonatedEmailAddr := s.GetArgs()["impersonatedUserEmail"].(string)

	credsJSON, _, err := pathOrContentsIsFile(credsPathOrJSON)
	if err != nil {
		return nil, fmt.Errorf("error reading credentials: %w", err)
	}

	tokenSource, err := s.getTokenSource([]byte(credsJSON), impersonatedEmailAddr)
	if err != nil {
		return nil, err
	}

	client := oauth2.NewClient(ctx, tokenSource)
	client.Timeout = 30 * time.Second

	svc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *GmailfilterService) validateCredentials(credsPathOrJSON string) error {
	credsJSON, isFile, err := pathOrContentsIsFile(credsPathOrJSON)
	if err != nil && isFile {
		return err
	}

	if _, err := googleoauth.CredentialsFromJSON(context.Background(), []byte(credsJSON), gmailAPIScopes...); err != nil {
		if isFile {
			return fmt.Errorf("JSON credentials in file %q are not valid: %w", credsPathOrJSON, err)
		}
		return fmt.Errorf("JSON credentials string is not valid: %w", err)
	}
	return nil
}

func (s *GmailfilterService) getTokenSource(credsJSON []byte, impersonatedEmailAddr string) (oauth2.TokenSource, error) {
	if len(impersonatedEmailAddr) > 0 {
		conf, err := googleoauth.JWTConfigFromJSON(credsJSON, gmailAPIScopes...)
		if err != nil {
			return nil, fmt.Errorf("unable to parse JWT config from JSON: %w", err)
		}
		conf.Subject = impersonatedEmailAddr
		return conf.TokenSource(context.Background()), nil
	}
	return googleoauth.DefaultTokenSource(context.Background(), gmailAPIScopes...)
}

type serviceAccountFile struct {
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
}

func parseJSON(result interface{}, contents string) error {
	r := strings.NewReader(contents)
	dec := json.NewDecoder(r)

	return dec.Decode(result)
}

func pathOrContentsIsFile(poc string) (string, bool, error) {
	if len(poc) == 0 {
		return poc, false, nil
	}

	path := poc
	if path[0] == '~' {
		var err error
		path, err = homedir.Expand(path)
		if err != nil {
			return path, true, fmt.Errorf("error expanding home directory for path %s: %w", poc, err)
		}
	}

	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		contents, err := os.ReadFile(path)
		if err != nil {
			return string(contents), true, fmt.Errorf("error reading file %s: %w", path, err)
		}
		return string(contents), true, nil
	}

	return poc, false, nil
}
