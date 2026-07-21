//go:build devtools

package pipelinespec

// BuiltinTestSeed is a developer-only pipeline method that emits a fixed list
// of topics for fan-out plumbing validation. It is compiled in only when the
// `devtools` build tag is set so operators cannot invoke it in production
// builds; pipelines that reference it will fail validation at load time.
const BuiltinTestSeed = "builtin_test_seed"

func init() {
	builtinMethodAliases[BuiltinTestSeed] = BuiltinTestSeed
	builtinMethodAliases["/test_seed"] = BuiltinTestSeed
	builtinMethodAliases["test_seed"] = BuiltinTestSeed
}
