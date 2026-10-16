// Package tabread reads upstream fpgrowth's "flexible" table/transaction
// format and its item selection and appearance files.
//
// It ports Christian Borgelt's util/src/tabread.c (the tokenizer),
// util/src/escape.c (escape decoding of the -r/-f/-b/-C option values) and
// the reading functions of tract/src/tract.c that fpgrowth uses
// (ib_read via tbg_read, ib_readsel, ib_readapp). See
// docs/upstream/cli-io.md sections 4 and 5 and docs/upstream/tract.md
// sections 2.3-2.5 for the upstream behavior.
//
// # Format
//
// The input is a sequence of bytes; four configurable byte classes
// (Config) give it structure. A record ends at a record separator, a field
// at a field separator or at the end of its record. Blanks before and
// after a field are dropped; a blank that is also a field separator
// merges with neighboring blanks, so "a  b" has two fields. A record whose
// first byte is a comment character is skipped entirely (comment records
// are still counted for record numbers). Empty and blank-only lines are
// records without fields (empty transactions). All bytes, including bytes
// >= 0x80, are plain bytes; UTF-8 is never interpreted.
//
// Fields are C strings upstream: everything after an embedded NUL byte is
// ignored, so an item name ends at its first NUL and a field starting with
// NUL counts as empty. This package applies the same rule.
//
// # C → Go mapping
//
//	trd_create, trd_allchs     Config, DefaultConfig (byte classes)
//	trd_read                   tokenizer.read (unexported)
//	esc_decode, esc_decstr     DecodeEscapes
//	ib_read (via tbg_read)     Reader.Next, Scan, ScanFile
//	ib_readsel                 ReadSelection
//	ib_readapp, appcode        ReadAppearances, Appearance
//	ib_errmsg                  SyntaxError.Error and the Err* sentinels
//
// Duplicate items within a transaction are not removed here (upstream's
// ib_read drops them): the consumer's item base ignores them, as
// tract.ScanFunc documents.
//
// # Deviations from upstream
//
// Each is marked "Deviation from upstream:" in the code and has a test.
//
//   - A final record that ends with a field separator directly followed by
//     the end of input (no record separator) is no longer discarded
//     (tract.md T3): the end of input then ends the record with an empty
//     last field, exactly as if a record separator followed. Upstream
//     already behaves so when a blank follows the separator ("a, " versus
//     "a,"). For selection and appearance files this makes "a," at the end
//     of input behave like "a,\n" (an error resp. an empty indicator).
//   - Record numbers in messages are always the number of the record the
//     offending field belongs to. Upstream reports the previous record
//     when the end of input directly follows trailing blanks (cli-io.md
//     B10).
//   - Fields are not truncated to 1024 bytes (cli-io.md B14); distinct
//     long names no longer merge. Error messages still quote at most the
//     first 1024 bytes of a field, as upstream does.
//   - A read error is reported with the file name (upstream prints the
//     current field instead, cli-io.md B8) and wraps the I/O error.
//   - Transaction weights are 64-bit (tract.Supp): values outside the
//     32-bit int range are kept instead of being truncated to int
//     (tract.md T6); values outside the 64-bit range are an invalid weight
//     instead of LONG_MAX/LONG_MIN truncated to int.
//   - Negative transaction weights are rejected (project integer policy;
//     upstream accepts them and produces meaningless results, cli-io.md
//     B12, tract.md T7).
//   - Escape decoding (DecodeEscapes): an unknown escape "\c" stands for
//     c (upstream returns the byte after c, or a backslash at the end of
//     the string); a lone trailing backslash stands for itself (upstream
//     reads past the end of the string); bytes >= 0x80 are ordinary bytes
//     and can be class members (upstream indexes its class table with a
//     negative value where char is signed). See cli-io.md B3 and B4.
package tabread
