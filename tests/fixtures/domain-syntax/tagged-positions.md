---
map: !!map &map
  key: value
sequence: !!seq
  - one
value: &value
  !!str
  text
literal: &lit
  !!str |

  content
alias: *map
tagged_empty: !!str
flow: {empty: , value: null}
---
Body.
