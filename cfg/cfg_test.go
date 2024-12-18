package cfg

import (
	"os"
	"path/filepath"
	"testing"
)

const configContent = `
{
  "host": "localhost",
  "port": 43210,
  "timeout": "10s",
  "workers": 1,
  "debug": true,
  "groups": [
    {
      "name": "group1",
      "endpoint": "/group1",
      "encoded": true,
      "period": "12h",
      "subscriptions": [
        {
          "name": "subscription1",
          "url": "http://localhost:43211/subscription1",
          "encoded": false,
          "timeout": "10s"
        },
        {
          "name": "subscription2",
          "url": "http://localhost:43212/subscription2",
          "encoded": true,
          "timeout": "10s"
        }
      ]
    }
  ]
}
`

func createConfigFile(name string) (string, error) {
	fullPath := filepath.Join(os.TempDir(), name)
	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return "", err
	}

	if _, err = f.Write([]byte(configContent)); err != nil {
		return "", err
	}

	if err = f.Close(); err != nil {
		return "", err
	}
	return fullPath, err
}

func TestNew(t *testing.T) {
	name, err := createConfigFile("smerge_test_new.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err = New("/bad_file_path.json"); err == nil {
		t.Error("unexpected behavior")
	}

	cfg, err := New(name)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Addr() != "localhost:43210" {
		t.Error("unexpected address")
	}
}
