import { stringify } from "yaml";
import type { components } from "@/api/schema";

// harnessYaml writes a stored harness as a harness file would hold it. Its
// policy is left out: the server stores the modules resolved, with package
// lines and file contents, so they cannot be written back as the file's
// policy section; the harness page shows them as Rego instead.
export function harnessYaml(harness: components["schemas"]["Harness"]): string {
  const { name, instructions, model, tools, limits } = harness;
  return stringify(
    {
      name,
      instructions,
      model: { provider: model.provider, name: model.name },
      ...(tools === undefined ? {} : { tools }),
      limits: { max_steps: limits.max_steps, max_tool_calls: limits.max_tool_calls },
    },
    { lineWidth: 0 },
  );
}
