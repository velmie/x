// Package confload loads typed configuration and describes its declared fields.
// Inspection reads decoded values using the same metadata, masks sensitive
// fields, and reports source labels without exposing configuration file paths.
// Paths exclude tag options, and unexported fields are not described. Duplicate
// paths, leaf/container conflicts, and invalid squash declarations fail both
// schema description and loading.
package confload
