# Positive Notes

- The loader validates the final merged config after applying `BN_STATUS_DEFAULT`, so invalid env overrides fail fast.
- The tests isolate `BN_CONFIG`, `BN_STATUS_DEFAULT`, `XDG_CONFIG_HOME`, and the working directory, which prevents ambient local config from influencing loader tests.

