//! Development-only probe of normal Cargo erasure and pinned Verus integration.
//! This does not implement either mandatory Beans kernel obligation.
use vstd::prelude::*;

verus! {
pub fn capped_increment(value: u8) -> (result: u8)
    ensures
        result >= value,
        value < 255 ==> result == value + 1,
        value == 255 ==> result == value,
{
    if value == 255 { value } else { value + 1 }
}
}

#[test]
fn executable_body_compiles_and_obeys_the_contract() {
    for value in 0..=u8::MAX {
        assert_eq!(capped_increment(value), value.saturating_add(1));
    }
}
