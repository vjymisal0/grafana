package frontend

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/ini.v1"

	"github.com/grafana/grafana/pkg/services/featuremgmt"
	"github.com/grafana/grafana/pkg/setting"
	"github.com/grafana/grafana/pkg/web"
)

// setupTestWebAssets creates a temporary directory with test assets manifest
func setupTestWebAssets(tb testing.TB) string {
	tb.Helper()

	publicDir := tb.TempDir()
	tb.Cleanup(func() { _ = os.RemoveAll(publicDir) })

	writeTestWebAssets(tb, publicDir, "build")

	return publicDir
}

// setupTestWebAssetsWithRspack creates a temporary directory holding both the webpack
// and the rspack assets, as a server would have during the rspack rollout
func setupTestWebAssetsWithRspack(tb testing.TB) string {
	tb.Helper()

	publicDir := setupTestWebAssets(tb)
	writeTestWebAssets(tb, publicDir, "build-rspack")

	return publicDir
}

// writeTestWebAssets writes a test assets manifest and boot script under the given
// build directory
func writeTestWebAssets(tb testing.TB, publicDir string, dir string) {
	tb.Helper()

	// Create build directory
	buildDir := filepath.Join(publicDir, dir)
	err := os.MkdirAll(buildDir, 0750)
	require.NoError(tb, err)

	// Create test assets manifest
	manifest := `{
		"entrypoints": {
			"app": {
				"assets": {
					"js": [
						"public/build/runtime.js",
						"public/build/app.js"
					],
					"css": ["public/build/grafana.app.css"]
				}
			},
			"swagger": {
				"assets": {
					"js": ["public/build/runtime.js", "public/build/swagger.js"],
					"css": ["public/build/grafana.swagger.css"]
				}
			},
			"dark": {
				"assets": {
					"css": ["public/build/grafana.dark.css"]
				}
			},
			"light": {
				"assets": {
					"css": ["public/build/grafana.light.css"]
				}
			}
		},
		"runtime.js": {
			"src": "public/build/runtime.js",
			"integrity": "sha256-test123"
		},
		"app.js": {
			"src": "public/build/app.js",
			"integrity": "sha256-test456"
		}
	}`
	manifest = strings.ReplaceAll(manifest, "public/build/", "public/"+dir+"/")

	err = os.WriteFile(filepath.Join(buildDir, "assets-manifest.json"), []byte(manifest), 0644)
	require.NoError(tb, err)

	err = os.WriteFile(filepath.Join(buildDir, "boot.js"), []byte("// test boot stub for "+dir), 0644)
	require.NoError(tb, err)
}

func TestFrontendService_WebAssets(t *testing.T) {
	t.Run("should serve index with proper assets", func(t *testing.T) {
		publicDir := setupTestWebAssets(t)
		cfg := &setting.Cfg{
			Raw:            ini.Empty(),
			HTTPPort:       "3000",
			StaticRootPath: publicDir,
			Env:            setting.Dev, // needs to be dev to bypass the cache
		}
		service := createTestService(t, cfg)

		mux := web.New()
		service.addMiddlewares(mux)
		service.registerRoutes(mux)

		// Test index route which should load web assets
		req := httptest.NewRequest("GET", "/", nil)
		recorder := httptest.NewRecorder()

		mux.ServeHTTP(recorder, req)

		assert.Equal(t, 200, recorder.Code)
		assert.Contains(t, recorder.Header().Get("Content-Type"), "text/html")
		assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")

		// The response should contain references to the assets
		body := recorder.Body.String()
		assert.Contains(t, body, "src=\"public/build/runtime.js\" type=\"text/javascript\"")
		assert.Contains(t, body, "src=\"public/build/app.js\" type=\"text/javascript\"")
	})

	t.Run("should serve rspack assets when the rspack flag is enabled", func(t *testing.T) {
		featuremgmt.WithEnabledFlags(t, featuremgmt.FlagGrafanaRspackBuild)

		publicDir := setupTestWebAssetsWithRspack(t)
		cfg := &setting.Cfg{
			Raw:            ini.Empty(),
			HTTPPort:       "3000",
			StaticRootPath: publicDir,
			Env:            setting.Dev, // needs to be dev to bypass the cache
		}
		service := createTestService(t, cfg)

		mux := web.New()
		service.addMiddlewares(mux)
		service.registerRoutes(mux)

		req := httptest.NewRequest("GET", "/", nil)
		recorder := httptest.NewRecorder()

		mux.ServeHTTP(recorder, req)

		assert.Equal(t, 200, recorder.Code)

		body := recorder.Body.String()
		assert.Contains(t, body, "src=\"public/build-rspack/runtime.js\" type=\"text/javascript\"")
		assert.Contains(t, body, "src=\"public/build-rspack/app.js\" type=\"text/javascript\"")
		assert.Contains(t, body, "// test boot stub for build-rspack")
	})

	t.Run("should fail the request when the rspack flag is on but the build is missing", func(t *testing.T) {
		featuremgmt.WithEnabledFlags(t, featuremgmt.FlagGrafanaRspackBuild)

		publicDir := setupTestWebAssets(t) // webpack assets only
		cfg := &setting.Cfg{
			Raw:            ini.Empty(),
			HTTPPort:       "3000",
			StaticRootPath: publicDir,
			Env:            setting.Dev,
		}
		service := createTestService(t, cfg)

		mux := web.New()
		service.addMiddlewares(mux)
		service.registerRoutes(mux)

		req := httptest.NewRequest("GET", "/", nil)
		recorder := httptest.NewRecorder()

		mux.ServeHTTP(recorder, req)

		assert.Equal(t, 500, recorder.Code)
	})
}
