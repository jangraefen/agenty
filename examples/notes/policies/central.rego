# Central policy: applies to every harness. A harness can tighten it, never
# loosen it.
package agenty.tool

writes := {"files_write_file", "files_edit_file", "files_move_file", "files_create_directory"}

require_approval contains "changes to files need a human" if input.tool in writes

deny contains "dotfiles may hold credentials" if {
	some part in split(input.args.path, "/")
	startswith(part, ".")
	part != "."
}
