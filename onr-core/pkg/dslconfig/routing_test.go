package dslconfig

import "testing"

func TestProviderRoutingReferencesVariable(t *testing.T) {
	routing := ProviderRouting{
		BaseURLExpr: `"https://api.example.com"`,
		Matches: []RoutingMatch{
			{
				API:     "chat.completions",
				SetPath: `template("/v1/projects/${credential.project_id}/locations/${channel.location}/chat/completions")`,
			},
		},
	}

	if !routing.ReferencesVariable("channel.location") {
		t.Fatalf("expected routing to reference channel.location")
	}
	if routing.ReferencesVariable("oauth.access_token") {
		t.Fatalf("routing should not reference oauth.access_token")
	}
}
