# Positive Notes

- `cmd/bn/workflow_test.go` covers `BN_CONFIG` precedence, current directory precedence, `.bn` marker discovery, XDG ordering, partial inheritance, and invalid override behavior.
- `cmd/bn/workflow.go` applies `BN_STATUS_DEFAULT` after file merge and before validation, matching the plan's intended override order.

