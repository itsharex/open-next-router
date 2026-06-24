package dslconfig

import (
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
)

func TestEvalStringExpr_TemplateForms(t *testing.T) {
	meta := &dslmeta.Meta{
		APIKey:              "sk-test",
		ChannelLocation:     "us-central1",
		CredentialProjectID: "vertex-project",
		OriginModelName:     "gpt-4o-mini",
		DSLModelMapped:      "mapped-model",
	}

	cases := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "double_quoted_template",
			expr: `template("/v1/${request.model_mapped}")`,
			want: "/v1/mapped-model",
		},
		{
			name: "single_quoted_template",
			expr: `template('/v1/${request.model_mapped}')`,
			want: "/v1/mapped-model",
		},
		{
			name: "escaped_placeholder",
			expr: `template("/literal/\${request.model_mapped}")`,
			want: "/literal/${request.model_mapped}",
		},
		{
			name: "concat_template",
			expr: `concat("Bearer ", template("${channel.key}"))`,
			want: "Bearer sk-test",
		},
		{
			name: "credential_and_channel",
			expr: `template("/v1/projects/${credential.project_id}/locations/${channel.location}/models/${$request.model_mapped}")`,
			want: "/v1/projects/vertex-project/locations/us-central1/models/mapped-model",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvalStringExpr(tc.expr, meta); got != tc.want {
				t.Fatalf("EvalStringExpr(%q)=%q want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestValidateStringExpr_TemplateRejectsUnknownVariable(t *testing.T) {
	err := ValidateStringExpr(`template("/v1/${request.unknown}")`)
	if err == nil {
		t.Fatalf("expected unsupported template variable error")
	}
}
