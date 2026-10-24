package reviewtransaction

import "testing"

func TestGeneratedCandidatePathRecognizesCodeGeneratorOutput(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "convex generated module", path: "packages/sdk/convex/actos/_generated/plantillas_cota.ts", want: true},
		{name: "convex generated api", path: "convex/_generated/api.d.ts", want: true},
		{name: "relay artifacts", path: "src/__generated__/UserQuery.graphql.ts", want: true},
		{name: "tanstack route tree", path: "apps/web/.tanstack/routeTree.gen.ts", want: true},
		{name: "generated infix", path: "src/client.generated.ts", want: true},
		{name: "kubernetes deepcopy", path: "api/v1/zz_generated.deepcopy.go", want: true},
		{name: "protobuf go", path: "proto/user.pb.go", want: true},
		{name: "protobuf python", path: "proto/user_pb2.py", want: true},
		{name: "grpc python", path: "proto/user_pb2_grpc.py", want: true},
		{name: "generator source", path: "scripts/build-plantillas-cota.ts", want: false},
		{name: "generated word in a directory name", path: "docs/generated-reports/summary.md", want: false},
		{name: "underscore directory near miss", path: "src/_generated_helpers/index.ts", want: false},
		{name: "gen prefix near miss", path: "src/general.ts", want: false},
		{name: "pb suffix near miss", path: "proto/user.pb.go.txt", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGeneratedCandidatePath(tt.path); got != tt.want {
				t.Fatalf("isGeneratedCandidatePath(%q) = %t, want %t", tt.path, got, tt.want)
			}
		})
	}
}
