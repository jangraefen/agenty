import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { type Api, unwrap } from "@/api/client";
import { harnessesQuery, harnessQuery } from "@/api/queries";
import { type HarnessValues, toHarness } from "@/lib/harness-form";

// useSaveHarness stores a harness from the form as its next version and opens
// it. Creating refuses a name another harness has: storing it would quietly
// make a new version of that one.
export function useSaveHarness(api: Api, workspace: string, creating: boolean) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);

  // Whether a harness has the name, by the workspace's list: asking for the
  // harness itself would answer 404 for a free name, which browsers log.
  async function exists(name: string): Promise<boolean> {
    const harnesses = await queryClient.fetchQuery({
      ...harnessesQuery(api, workspace),
      staleTime: 0,
    });
    return harnesses.some((version) => version.harness.name === name);
  }

  async function save(values: HarnessValues) {
    const harness = toHarness(values);
    const path = { params: { path: { workspace, name: harness.name } } };
    setError(null);
    try {
      if (creating && (await exists(harness.name))) {
        setError(`A harness named ${harness.name} exists already; edit it instead.`);
        return;
      }
      const stored = await unwrap(
        api.PUT("/v1/workspaces/{workspace}/harnesses/{name}", { ...path, body: harness }),
      );
      queryClient.setQueryData(harnessQuery(api, workspace, harness.name).queryKey, stored);
      await queryClient.invalidateQueries({ queryKey: harnessesQuery(api, workspace).queryKey });
      await navigate({
        to: "/w/$workspace/harnesses/$name",
        params: { workspace, name: harness.name },
      });
    } catch (cause) {
      setError(
        `The harness could not be saved: ${cause instanceof Error ? cause.message : String(cause)}`,
      );
    }
  }

  return { save, error };
}
