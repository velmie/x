# Legacy case disposition

This inventory covers the source tests in the legacy dynamicdata package. Retained expression behavior is exercised through the public API in the Go tests. Constructed objects now use `map[string]any`; error wrapping and numeric literal types follow this module's documented contract. Registry lifecycle, annotations, conversion helpers and map utilities remain outside this module.

| Source case | Disposition |
| --- | --- |
| `annotation_test.go::TestAnnotations` | Dropped: utility or conversion policy remains with the source owner. |
| `TestAnnotations/add and get existing annotation` | Dropped: utility or conversion policy remains with the source owner. |
| `TestAnnotations/get non-existing annotation` | Dropped: utility or conversion policy remains with the source owner. |
| `combine_test.go::TestCombinePathResolver_Resolve` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Simple path resolution` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Complex combine path resolution` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Complex combine path resolution with nested objects` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Deeply nested` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Optional elements` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Omit empty` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Omit empty with not empty fields` | Retained or changed: projection behavior uses real roots and fresh ordinary maps; injected resolver failures are replaced by real traversal errors. |
| `TestCombinePathResolver_Resolve/Resolver err` | Changed: a real traversal failure replaces the injected mock failure. |
| `index_test.go::TestIndexResolver_Resolve` | Retained or changed: typed slice lookup and missing bounds; failures return nil and typed diagnostics, with no resolver forwarding. |
| `TestIndexResolver_Resolve/Resolve index for existing key in slice` | Retained or changed: typed slice lookup and missing bounds; failures return nil and typed diagnostics, with no resolver forwarding. |
| `TestIndexResolver_Resolve/Resolve with invalid index - out of bounds` | Retained or changed: typed slice lookup and missing bounds; failures return nil and typed diagnostics, with no resolver forwarding. |
| `TestIndexResolver_Resolve/Resolve for non-slice data` | Changed: returns nil and a type error rather than the offending value. |
| `TestIndexResolver_Resolve/Forward to base resolver when path has no index` | Retained as direct plain-key lookup; resolver forwarding is removed. |
| `literal_test.go::TestLiteralResolver_Resolve` | Retained or changed: literals and lookup use compiled expressions; numeric results are `json.Number`, invalid strings and empty expressions fail at compile time. |
| `TestLiteralResolver_Resolve/Resolve true literal` | Retained or changed: literals and lookup use compiled expressions; numeric results are `json.Number`, invalid strings and empty expressions fail at compile time. |
| `TestLiteralResolver_Resolve/Resolve integer literal` | Changed: integer literal is a `json.Number` preserving its spelling. |
| `TestLiteralResolver_Resolve/Forward to base resolver for non-literal` | Retained or changed: literals and lookup use compiled expressions; numeric results are `json.Number`, invalid strings and empty expressions fail at compile time. |
| `TestLiteralResolver_Resolve/Float literal resolution` | Changed: float literal is a `json.Number` preserving its spelling. |
| `TestLiteralResolver_Resolve/String literal enclosed in single quotes` | Retained or changed: literals and lookup use compiled expressions; numeric results are `json.Number`, invalid strings and empty expressions fail at compile time. |
| `TestLiteralResolver_Resolve/Boolean false literal resolution` | Retained or changed: literals and lookup use compiled expressions; numeric results are `json.Number`, invalid strings and empty expressions fail at compile time. |
| `TestLiteralResolver_Resolve/Single character string resolution` | Retained or changed: literals and lookup use compiled expressions; numeric results are `json.Number`, invalid strings and empty expressions fail at compile time. |
| `TestLiteralResolver_Resolve/Edge case with empty path` | Changed: empty expression fails compilation. |
| `TestLiteralResolver_Resolve/Handling of special characters within string literals` | Changed: unescaped interior quotes fail compilation. |
| `map_test.go::TestMerge` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMerge/Empty Maps` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMerge/One Empty Map` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMerge/Same Keys` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMerge/Different Keys` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMerge/Call on nil map` | Dropped: utility or conversion policy remains with the source owner. |
| `map_test.go::TestDeepMerge` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/Empty Maps` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/One Empty Map` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/Same Keys` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/Different Keys` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/Nested Maps` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/Nested Maps Different Types` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMerge/Call on nil map` | Dropped: utility or conversion policy remains with the source owner. |
| `map_test.go::TestDeepMergeDifferentLevels` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDeepMergeDifferentLevels/Different Levels` | Dropped: utility or conversion policy remains with the source owner. |
| `map_test.go::TestNestedData` | Dropped: utility or conversion policy remains with the source owner. |
| `TestNestedData/No Such Path` | Dropped: utility or conversion policy remains with the source owner. |
| `TestNestedData/Nested Map` | Dropped: utility or conversion policy remains with the source owner. |
| `map_test.go::TestDereference` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDereference/Flat Map` | Dropped: utility or conversion policy remains with the source owner. |
| `TestDereference/Nested Map` | Dropped: utility or conversion policy remains with the source owner. |
| `map_test.go::TestNestedDataNonMap` | Dropped: utility or conversion policy remains with the source owner. |
| `TestNestedDataNonMap/Non-Map Value` | Dropped: utility or conversion policy remains with the source owner. |
| `map_test.go::TestMergeInPlace` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMergeInPlace/Empty Maps` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMergeInPlace/One Empty Map` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMergeInPlace/Same Keys` | Dropped: utility or conversion policy remains with the source owner. |
| `TestMergeInPlace/Different Keys` | Dropped: utility or conversion policy remains with the source owner. |
| `registry_test.go::TestRegistryRegister` | Dropped: registry lifecycle and policy remain with the source owner. |
| `registry_test.go::TestRegistryData` | Changed: source-relative and named-root lookup retained; registry creation remains outside the module. |
| `registry_test.go::TestRegistryAll` | Dropped: registry lifecycle and policy remain with the source owner. |
| `registry_test.go::TestRegistryValue` | Changed: source selection remains separate from compiled path lookup. |
| `registry_test.go::TestRegistryAnnotations` | Dropped: registry lifecycle and policy remain with the source owner. |
| `registry_test.go::TestRegistryCoalesce` | Dropped: registry lifecycle and policy remain with the source owner. |
| `TestRegistryCoalesce/empty args` | Dropped: registry lifecycle and policy remain with the source owner. |
| `TestRegistryCoalesce/odd number of args` | Dropped: registry lifecycle and policy remain with the source owner. |
| `TestRegistryCoalesce/source not found` | Dropped: registry lifecycle and policy remain with the source owner. |
| `TestRegistryCoalesce/value found, one source` | Dropped: registry lifecycle and policy remain with the source owner. |
| `TestRegistryCoalesce/value found, multiple sources` | Dropped: registry lifecycle and policy remain with the source owner. |
| `registry_test.go::TestRegistrySet` | Dropped: registry lifecycle and policy remain with the source owner. |
| `resolver_test.go::TestNewDotPathResolver` | Dropped: removed resolver constructors. |
| `resolver_test.go::TestNewDelimiterPathResolver` | Dropped: removed resolver constructors. |
| `resolver_test.go::TestResolve` | Retained through compiled expressions or paths, with documented error and container types. |
| `TestResolve/root` | Retained through compiled expressions or paths, with documented error and container types. |
| `TestResolve/simple key` | Retained through compiled expressions or paths, with documented error and container types. |
| `TestResolve/nested key` | Retained through compiled expressions or paths, with documented error and container types. |
| `TestResolve/multi-level key` | Retained through compiled expressions or paths, with documented error and container types. |
| `TestResolve/wrong container` | Retained through compiled expressions or paths, with documented error and container types. |
| `TestResolve/missing key` | Retained through compiled expressions or paths, with documented error and container types. |
| `resolver_test.go::TestSet` | Retained: simple and nested assignment; invalid intermediates fail without mutation. |
| `TestSet/Set value on simple path` | Retained: simple and nested assignment; invalid intermediates fail without mutation. |
| `TestSet/Set value on nested path` | Retained: simple and nested assignment; invalid intermediates fail without mutation. |
| `TestSet/Handle non-map intermediate path segment` | Retained: simple and nested assignment; invalid intermediates fail without mutation. |
| `standard_resolver_test.go::TestStandardPathResolver` | Retained through compiled expressions or paths, with documented error and container types. |
| `standard_resolver_test.go::TestStandardPathResolverRejectsInvalidProjection` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{?}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{??}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:?}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:??}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{,claim:true}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:true,,other:true}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:true,}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:true,claim:false}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:missing?,claim:true}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:{nested:true}junk}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `TestStandardPathResolverRejectsInvalidProjection/{claim:{nested:true}` | Changed: invalid expressions fail during compilation rather than evaluation. |
| `standard_resolver_test.go::TestStandardPathResolverIndexBounds` | Changed: negative indexes fail compilation; nonnegative bounds wrap `ErrNotFound`. |
| `TestStandardPathResolverIndexBounds/roles[-1]` | Changed: a negative index fails compilation. |
| `TestStandardPathResolverIndexBounds/roles[1]` | Changed: negative indexes fail compilation; nonnegative bounds wrap `ErrNotFound`. |
| `TestStandardPathResolverIndexBounds/roles[2]` | Changed: negative indexes fail compilation; nonnegative bounds wrap `ErrNotFound`. |
| `TestStandardPathResolverIndexBounds/empty[0]` | Changed: negative indexes fail compilation; nonnegative bounds wrap `ErrNotFound`. |
| `standard_resolver_test.go::TestStandardPathResolverNestedErrorDoesNotExposeData` | Retained through compiled expressions or paths, with documented error and container types. |
| `standard_resolver_test.go::TestStandardPathResolverOptionalValues` | Retained through compiled expressions or paths, with documented error and container types. |
| `standard_resolver_test.go::TestStandardPathResolverEmptyProjection` | Retained through compiled expressions or paths, with documented error and container types. |
| `value_test.go::TestValue` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/String should return correct string value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/String called on a not string value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/StringSlice should return correct string slice value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/StringSlice called on a not string slice value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Int32 should return correct int32 value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Int32 called on a not int32 value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Int64 should return correct int64 value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Int64 called on a not int64 value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Float32 should return correct float32 value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Float32 called on a not float32 value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Float64 should return correct float64 value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Float64 called on a not float64 value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/MapStringAny should return correct map[string]any value` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/MapStringAny should return correct map[string]any value from Map` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/MapStringAny called on a not map[string]any value should return error` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is true` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is false` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is string 'true'` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is string 'false'` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is int32 1` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is int32 0` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is int64 1` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is int64 0` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is float32 0` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is float32 1` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is float64 0` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool should return correct bool value when input is float64 1` | Dropped: utility or conversion policy remains with the source owner. |
| `TestValue/Bool called on a not boolean value should return error` | Dropped: utility or conversion policy remains with the source owner. |

The following table records the individual cases in legacy table-driven tests
that did not use named subtests:

| Source case | Disposition |
| --- | --- |
| `TestRegistryRegister/first registration` | Dropped: registry lifecycle remains with the source owner. |
| `TestRegistryRegister/duplicate registration` | Dropped: duplicate-source policy remains with the source owner. |
| `TestRegistryData/existing source` | Changed: the caller selects the source; its data can be supplied as a root. |
| `TestRegistryData/missing source` | Dropped: source selection remains with the caller. |
| `TestRegistryValue/existing source and key` | Changed: compile and get the path from the selected source. |
| `TestRegistryValue/missing key` | Retained as a path lookup wrapping `ErrNotFound`. |
| `TestRegistryValue/missing source` | Dropped: source selection remains with the caller. |
| `TestRegistryAnnotations/existing source` | Dropped: annotations remain with the source owner. |
| `TestRegistryAnnotations/missing source` | Dropped: annotations and source selection remain with the source owner. |
