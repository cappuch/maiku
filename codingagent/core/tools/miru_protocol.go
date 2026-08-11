package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikus/maiku/ai/auth"
	miru "github.com/takara-ai/miru-code"
)

// MiruRequest/MiruResponse are the private in-process protocol between the
// agent and the bundled Miru service. No MCP process or socket is involved.
type MiruRequest struct {
	Query string `json:"query"`
	Path  string `json:"path"`
	Limit int    `json:"limit"`
}

type MiruResponse struct {
	Query   string              `json:"query"`
	Results []miru.SearchResult `json:"results"`
}

type MiruService interface {
	Search(context.Context, MiruRequest) (MiruResponse, error)
}

type localMiruService struct{ cwd string }

func NewMiruService(cwd string) MiruService { return localMiruService{cwd: cwd} }

func (s localMiruService) Search(ctx context.Context, request MiruRequest) (MiruResponse, error) {
	if err := checkAborted(ctx); err != nil {
		return MiruResponse{}, err
	}
	if os.Getenv("TAKARA_API_KEY") == "" {
		if key := auth.ResolveAPIKey("miru"); key != "" {
			_ = os.Setenv("TAKARA_API_KEY", key)
		}
	}
	absPath, err := filepath.Abs(ResolveToCwd(request.Path, s.cwd))
	if err != nil {
		return MiruResponse{}, err
	}
	idx, err := miru.FromPath(absPath, []miru.ContentType{miru.ContentCode})
	if err != nil {
		return MiruResponse{}, fmt.Errorf("index: %w", err)
	}
	results, err := idx.Search(request.Query, request.Limit, nil, nil, nil, nil)
	if err != nil {
		return MiruResponse{}, fmt.Errorf("search: %w", err)
	}
	return MiruResponse{Query: request.Query, Results: results}, nil
}

func encodeMiruResponse(response MiruResponse) (string, error) {
	encoded, err := json.Marshal(response)
	return string(encoded), err
}
