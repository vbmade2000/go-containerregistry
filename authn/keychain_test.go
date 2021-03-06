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

package authn

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/google/go-containerregistry/name"
)

func TestConfigDir(t *testing.T) {
	notSet, err := configDir()
	if err != nil {
		t.Errorf("configDir() = %v", err)
	}

	// Now set it to something specific and try again.
	want := "/path/to/.docker"
	os.Setenv("DOCKER_CONFIG", want)

	set, err := configDir()
	if err != nil {
		t.Errorf("configDir() = %v", err)
	}

	// the "set" version, should match what we want.
	if set != want {
		t.Errorf("configDir(set); got %v, want %v", set, want)
	}

	// the "notSet" version, shouldn't match what we got after setting it.
	if notSet == set {
		t.Errorf("configDir(not set) = %v", notSet)
	}
}

var (
	fresh           = 0
	testRegistry, _ = name.NewRegistry("test.io", name.WeakValidation)
)

// setupConfigDir sets up an isolated configDir() for this test.
func setupConfigDir() string {
	fresh = fresh + 1
	p := fmt.Sprintf("%s/%d", os.Getenv("TEST_TMPDIR"), fresh)
	os.Setenv("DOCKER_CONFIG", p)
	if err := os.Mkdir(p, 0777); err != nil {
		panic(err)
	}
	return p
}

func setupConfigFile(content string) {
	p := path.Join(setupConfigDir(), "config.json")
	if err := ioutil.WriteFile(p, []byte(content), 0600); err != nil {
		panic(err)
	}
}

func checkOutput(t *testing.T, want string) {
	auth, err := DefaultKeychain.Resolve(testRegistry)
	if err != nil {
		t.Errorf("Resolve() = %v", err)
	}

	got, err := auth.Authorization()
	if err != nil {
		t.Errorf("Authorization() = %v", err)
	}
	if got != want {
		t.Errorf("Authorization(); got %v, want %v", got, want)
	}
}

func checkAnonymousFallback(t *testing.T) {
	checkOutput(t, "")
}

func checkFooBarOutput(t *testing.T) {
	// base64(foo:bar)
	checkOutput(t, "Basic Zm9vOmJhcg==")
}

func checkHelper(t *testing.T) {
	auth, err := DefaultKeychain.Resolve(testRegistry)
	if err != nil {
		t.Errorf("Resolve() = %v", err)
	}

	help, ok := auth.(*helper)
	if !ok {
		t.Errorf("Resolve(); got %T, want *helper", auth)
	}
	if help.name != "test" {
		t.Errorf("Resolve().name; got %v, want \"test\"", help.name)
	}
	if help.domain != testRegistry {
		t.Errorf("Resolve().domain; got %v, want %v", help.domain, testRegistry)
	}
}

func TestNoConfig(t *testing.T) {
	setupConfigDir()

	checkAnonymousFallback(t)
}

func TestVariousPaths(t *testing.T) {
	tests := []struct {
		content string
		check   func(*testing.T)
	}{{
		content: `}{`,
		check:   checkAnonymousFallback,
	}, {
		content: `{"credHelpers": {"https://test.io": "test"}}`,
		check:   checkHelper,
	}, {
		content: `{"credStore": "test"}`,
		check:   checkHelper,
	}, {
		content: `{"auths": {"http://test.io/v2/": {"auth": "Zm9vOmJhcg=="}}}`,
		check:   checkFooBarOutput,
	}, {
		content: `{"auths": {"https://test.io/v1/": {"username": "foo", "password": "bar"}}}`,
		check:   checkFooBarOutput,
	}, {
		content: `{"auths": {"other.io": {"username": "asdf", "password": "fdsa"}}}`,
		check:   checkAnonymousFallback,
	}}

	for _, test := range tests {
		setupConfigFile(test.content)

		test.check(t)
	}
}
