// Package native holds what the module's native tier shares across its layers: the check of a
// request's Native field, the lookup of a member in it, and the encoding of a wire body that
// merges it, which protocol, the formats, and the client's operations outside the Format each
// need and none can import from another.
//
// Its exported identifiers:
//
//   - [Valid] reports whether a value can be a Native field: empty, or a JSON object with
//     unique member names.
//   - [Has] reports whether a Native field has a member of a name.
//   - [Marshal] encodes a wire body that embeds a Native field, refusing a member that clashes
//     with one the body's typed fields set.
package native
