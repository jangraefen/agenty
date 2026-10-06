import { stringify } from "yaml";
import type { components } from "@/api/schema";

// harnessYaml writes a stored harness as a harness file would hold it. Its
// policy is left out: the server stores the modules resolved, with package
// lines and file contents, so they cannot be written back as the file's
// policy section; the harness page shows them as Rego instead. A comment
// says so, so a copy applied as it is does not quietly drop the rules.
export function harnessYaml(harness: components["schemas"]["Harness"]): string {
  const { name, instructions, model, tools, limits, policy = [] } = harness;
  const warning =
    policy.length === 0
      ? ""
      : `# policy: left out. This harness has ${policy.length} policy ${policy.length === 1 ? "module" : "modules"}, shown on its page as Rego;\n` +
        "# applying this YAML without it would drop its rules.\n";
  const yaml = stringify(
    {
      name,
      instructions,
      model: { provider: model.provider, name: model.name },
      ...(tools === undefined ? {} : { tools }),
      limits: { max_steps: limits.max_steps, max_tool_calls: limits.max_tool_calls },
    },
    { lineWidth: 0 },
  );
  return yaml + warning;
}
