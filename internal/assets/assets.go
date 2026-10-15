// Package assets embeds the default templates, tool configs and the
// Drizzle -> TypeSpec script.
package assets

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

var (
	//go:embed templates/api.mustache
	APIMustache string
	//go:embed templates/model.mustache
	ModelMustache string
	//go:embed tspconfig.yaml
	TSPConfig string
	//go:embed oaf-config.yaml
	OAFConfig string
	//go:embed drizzle-to-tsp.mts
	DrizzleToTSP string
)

// DrizzleScriptHash is a short content hash used in the extracted file name.
func DrizzleScriptHash() string {
	h := sha256.Sum256([]byte(DrizzleToTSP))
	return hex.EncodeToString(h[:])[:12]
}

// WriteTemplates extracts the embedded mustache templates into dir, rewriting
// the hard-coded `models.v1.` prefix for another proto package version.
func WriteTemplates(dir, version string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, body := range map[string]string{"api.mustache": APIMustache, "model.mustache": ModelMustache} {
		if version != "v1" {
			body = strings.ReplaceAll(body, "models.v1.", "models."+version+".")
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
