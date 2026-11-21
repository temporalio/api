package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRPCEndpointAllowlist(t *testing.T) {
	t.Parallel()
	const name = "fixture.Request"
	first := rpcEndpointUse{position: rpcPosition{"fixture.First.Call", "request"}, stable: true}
	second := rpcEndpointUse{position: rpcPosition{"fixture.Second.Call", "response"}, stable: true}
	allowlist := rpcEndpointPositions{
		name: {first.position: {}, second.position: {}},
	}
	additional := rpcEndpointUse{position: rpcPosition{"fixture.Second.Call", "request"}, stable: true}
	draft := second
	draft.stable = false
	const unlistedError = "fixture.Second.Call request: fixture.Request is not allowlisted at this RPC endpoint position"
	const draftError = "fixture.Second.Call response: fixture.Request is allowlisted only for stable RPC endpoints"

	for _, tc := range []struct {
		name     string
		uses     []rpcEndpointUse
		wantErrs []string
	}{
		{"listed positions", []rpcEndpointUse{first, second}, []string{"", ""}},
		{"listed positions reversed", []rpcEndpointUse{second, first}, []string{"", ""}},
		{"unlisted singleton", []rpcEndpointUse{additional}, []string{unlistedError}},
		{"unlisted before listed", []rpcEndpointUse{additional, first}, []string{unlistedError, ""}},
		{"unlisted after listed", []rpcEndpointUse{first, additional}, []string{"", unlistedError}},
		{"draft singleton", []rpcEndpointUse{draft}, []string{draftError}},
		{"draft after listed", []rpcEndpointUse{first, draft}, []string{"", draftError}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uses := make(rpcEndpointUses)
			for i, use := range tc.uses {
				err := uses.add(name, use, allowlist)
				want := tc.wantErrs[i]
				if want == "" && err != nil {
					t.Fatalf("use %d: %v", i+1, err)
				}
				if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
					t.Fatalf("use %d: want error containing %q, got %v", i+1, want, err)
				}
			}
		})
	}
}

func TestRPCEndpointAllowlistSource(t *testing.T) {
	t.Parallel()
	cfg := testConfig
	cfg.rpcEndpointAllowlist = rpcEndpointPositions{
		"fixture.Shared": {
			{"fixture.First.Call", "request"}:   {},
			{"fixture.First.Call", "response"}:  {},
			{"fixture.Second.Call", "response"}: {},
		},
	}
	schema, err := os.ReadFile(filepath.Join("../..", cfg.draftSchema))
	if err != nil {
		t.Fatal(err)
	}

	const preamble = `syntax = "proto3";
package fixture;
import "temporal/api_next/protometa/v1/draft.proto";
message Request {}
message Response {}
`
	const draftEndpoints = `message Shared { option (temporal.api.protometa.v1.draft_message) = "preview"; }
message DraftResponse { option (temporal.api.protometa.v1.draft_message) = "preview"; }
`
	for _, tc := range []struct {
		name, source, wantError string
	}{
		{
			name: "listed stable positions",
			source: `message Shared {}
service First { rpc Call(Shared) returns (Response); }
service Second { rpc Call(Request) returns (Shared); }
`,
		},
		{
			name: "moved singleton",
			source: `message Shared {}
service Second { rpc Call(Shared) returns (Response); }
`,
			wantError: "fixture.Second.Call request: fixture.Shared is not allowlisted at this RPC endpoint position",
		},
		{
			name: "both positions of one RPC",
			source: `message Shared {}
service First { rpc Call(Shared) returns (Shared); }
`,
			wantError: "fixture.First.Call: RPC uses fixture.Shared as both request and response",
		},
		{
			name: "draft method at a listed position",
			source: draftEndpoints + `service First {
  rpc Stable(Request) returns (Response);
  rpc Call(Shared) returns (DraftResponse) {
    option (temporal.api.protometa.v1.draft_method) = "preview";
  }
}
`,
			wantError: "fixture.First.Call request: fixture.Shared is allowlisted only for stable RPC endpoints",
		},
		{
			name: "draft service at a listed position",
			source: draftEndpoints + `service First {
  option (temporal.api.protometa.v1.draft_service) = "preview";
  rpc Call(Shared) returns (DraftResponse);
}
`,
			wantError: "fixture.First.Call request: fixture.Shared is allowlisted only for stable RPC endpoints",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, cfg.draftSchema), string(schema))
			writeFile(t, filepath.Join(root, cfg.nextDir, "fixture.proto"), preamble+tc.source)
			err := cfg.stableProtosGenerator(t.Context(), root, t.TempDir())
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("want error containing %q, got %v", tc.wantError, err)
			}
		})
	}
}
