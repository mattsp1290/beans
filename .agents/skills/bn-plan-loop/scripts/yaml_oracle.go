// Command yaml_oracle parses one contract document with Beans' YAML library.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func rejectUnsafe(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("YAML aliases are not allowed")
	}
	if node.Tag != "" && node.Tag[0] == '!' && len(node.Tag) > 1 && node.Tag[:2] != "!!" {
		return fmt.Errorf("custom YAML tags are not allowed")
	}
	for _, child := range node.Content {
		if err := rejectUnsafe(child); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := rejectUnsafe(&node); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var value map[string]any
	if err := node.Decode(&value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		panic(err)
	}
}
