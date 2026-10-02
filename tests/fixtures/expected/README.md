# Native expected values

These static cases define lossless bytes, semantic models, validation, filesystem
effects and command registration used by native Rust tests. They are test inputs,
not scripts or executable reference clients. Historical origin hashes are retained
in each corpus. Historical authored prose inside sample documents is opaque
fixture data and does not select a build, toolchain or behavior.

Expected changes require a stored-format or native-behavior justification and a
meaningful assertion. Do not regenerate them from the implementation under test.
