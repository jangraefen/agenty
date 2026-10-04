// Checks the licenses of all installed npm packages against the allowlists (ARCHITECTURE §16).
// Package data comes from `pnpm licenses list --json`, which covers every package in the
// lockfile that is installed for this platform, including optional native binaries.
// Exits non-zero and names each offending package and its license.
// Keep the allowlists in line with the Go allowlist in go-licenses-allowlist.txt.
import { execFileSync } from "node:child_process"

// Production dependencies are shipped in the built assets and must be permissive.
const SHIPPED = ["MIT", "ISC", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "0BSD"]

// All dependencies, including build and test tools, which are not distributed.
const ALL = [...SHIPPED, "MIT-0", "BlueOak-1.0.0", "CC0-1.0", "MPL-2.0"]

/**
 * Reports whether an SPDX license expression is satisfied by the allowlist:
 * `A OR B` needs one allowed side, `A AND B` needs both. Anything that is not a
 * well-formed expression of allowed identifiers is rejected.
 * @param {string} expression
 * @param {string[]} allowed
 * @returns {boolean}
 */
function isAllowed(expression, allowed) {
	const tokens = expression.match(/\(|\)|[^\s()]+/g) ?? []
	let pos = 0
	/** @returns {boolean} */
	const primary = () => {
		const token = tokens[pos++]
		if (token === "(") {
			const value = or()
			if (tokens[pos++] !== ")") throw new Error("unbalanced parentheses")
			return value
		}
		if (token === undefined || token === ")" || token === "AND" || token === "OR") {
			throw new Error("license identifier expected")
		}
		return allowed.includes(token)
	}
	/** @returns {boolean} */
	const and = () => {
		let value = primary()
		while (tokens[pos] === "AND") {
			pos++
			value = primary() && value
		}
		return value
	}
	/** @returns {boolean} */
	const or = () => {
		let value = and()
		while (tokens[pos] === "OR") {
			pos++
			value = and() || value
		}
		return value
	}
	try {
		const value = or()
		return pos === tokens.length && value
	} catch {
		return false
	}
}

/**
 * Lists the installed packages and returns those whose license is not allowed.
 * @param {string[]} pnpmArgs extra arguments for `pnpm licenses list`
 * @param {string[]} allowed
 * @returns {string[]} one "name@version: license" line per violation
 */
function violations(pnpmArgs, allowed) {
	const output = execFileSync("pnpm", ["licenses", "list", "--json", ...pnpmArgs], { encoding: "utf8" })
	/** @type {Record<string, {name: string, versions: string[], license: string}[]>} */
	const byLicense = JSON.parse(output.trim() || "{}")
	const found = []
	for (const [license, packages] of Object.entries(byLicense)) {
		if (isAllowed(license, allowed)) continue
		for (const pkg of packages) found.push(`${pkg.name}@${pkg.versions.join(", ")}: ${license}`)
	}
	return found
}

let failed = false
for (const [scope, args, allowed] of [
	["shipped (production)", ["--prod"], SHIPPED],
	["all", [], ALL],
]) {
	const found = violations(/** @type {string[]} */ (args), /** @type {string[]} */ (allowed))
	if (found.length === 0) {
		console.log(`ok      licenses ${scope}`)
		continue
	}
	failed = true
	console.error(`FAILED  licenses ${scope}: not in the allowlist (${allowed.join(", ")})`)
	for (const line of found) console.error(`  ${line}`)
}
process.exitCode = failed ? 1 : 0
