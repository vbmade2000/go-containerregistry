// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remote

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"

	"github.com/google/go-containerregistry/authn"
	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1"
	"github.com/google/go-containerregistry/v1/partial"
	"github.com/google/go-containerregistry/v1/remote/transport"
	"github.com/google/go-containerregistry/v1/types"
	"github.com/google/go-containerregistry/v1/v1util"
)

// remoteImage accesses an image from a remote registry
type remoteImage struct {
	ref          name.Reference
	client       *http.Client
	manifestLock sync.Mutex // Protects manifest
	manifest     []byte
	configLock   sync.Mutex // Protects config
	config       []byte
}

var _ partial.CompressedImageCore = (*remoteImage)(nil)

// Image accesses a given image reference over the provided transport, with the provided authentication.
func Image(ref name.Reference, auth authn.Authenticator, t http.RoundTripper) (v1.Image, error) {
	tr, err := transport.New(ref, auth, t, transport.PullScope)
	if err != nil {
		return nil, err
	}
	return partial.CompressedToImage(&remoteImage{
		ref:    ref,
		client: &http.Client{Transport: tr},
	})
}

func (r *remoteImage) url(resource, identifier string) url.URL {
	return url.URL{
		Scheme: transport.Scheme(r.ref.Context().Registry),
		Host:   r.ref.Context().RegistryStr(),
		Path:   fmt.Sprintf("/v2/%s/%s/%s", r.ref.Context().RepositoryStr(), resource, identifier),
	}
}

func (r *remoteImage) MediaType() (types.MediaType, error) {
	// TODO(jonjohnsonjr): Determine this based on response.
	return types.DockerManifestSchema2, nil
}

// TODO(jonjohnsonjr): Handle manifest lists.
// TODO(jonjohnsonjr): DockerHub returns the manifest list's digest when it falls back to schema 2??
func (r *remoteImage) RawManifest() ([]byte, error) {
	r.manifestLock.Lock()
	defer r.manifestLock.Unlock()
	if r.manifest != nil {
		return r.manifest, nil
	}

	u := r.url("manifests", r.ref.Identifier())
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// TODO(jonjohnsonjr): Accept OCI manifest, manifest list, and image index.
	req.Header.Set("Accept", string(types.DockerManifestSchema2))
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkError(resp, http.StatusOK); err != nil {
		return nil, err
	}

	manifest, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	digest, _, err := v1.SHA256(ioutil.NopCloser(bytes.NewReader(manifest)))
	if err != nil {
		return nil, err
	}

	// Validate the digest matches what we asked for, if pulling by digest.
	if dgst, ok := r.ref.(name.Digest); ok {
		if digest.String() != dgst.DigestStr() {
			return nil, fmt.Errorf("manifest digest: %s does not match requested digest: %s", digest, dgst.DigestStr())
		}
	} else if checksum := resp.Header.Get("Docker-Content-Digest"); checksum != "" && checksum != digest.String() {
		// When pulling by tag, we can only validate that the digest matches what the registry told us it should be.
		return nil, fmt.Errorf("manifest digest: %s does not match Docker-Content-Digest: %s", digest, checksum)
	}

	r.manifest = manifest
	return r.manifest, nil
}

func (r *remoteImage) RawConfigFile() ([]byte, error) {
	r.configLock.Lock()
	defer r.configLock.Unlock()
	if r.config != nil {
		return r.config, nil
	}

	m, err := partial.Manifest(r)
	if err != nil {
		return nil, err
	}

	body, err := r.Blob(m.Config.Digest)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	r.config, err = ioutil.ReadAll(body)
	if err != nil {
		return nil, err
	}
	return r.config, nil
}

func (r *remoteImage) Blob(h v1.Hash) (io.ReadCloser, error) {
	u := r.url("blobs", h.String())
	resp, err := r.client.Get(u.String())
	if err != nil {
		return nil, err
	}

	if err := checkError(resp, http.StatusOK); err != nil {
		resp.Body.Close()
		return nil, err
	}

	return v1util.VerifyReadCloser(resp.Body, h)
}
