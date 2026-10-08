import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type Api, unwrap } from "@/api/client";
import { harnessesQuery, harnessQuery } from "@/api/queries";
import { type HarnessValues, toHarness } from "@/lib/harness-form";

// The error of creating a harness under a name another harness has.
class HarnessExists extends Error {}

// useSaveHarness stores a harness from the form as its next version and opens
// it. Creating refuses a name another harness has: storing it would quietly
// make a new version of that one. error says why the last save failed, null
// while saving and after a save that did not.
export function useSaveHarness(api: Api, workspace: string, creating: boolean) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  // Whether a harness has the name, by the workspace's list: asking for the
  // harness itself would answer 404 for a free name, which browsers log.
  async function exists(name: string): Promise<boolean> {
    const harnesses = await queryClient.fetchQuery({
      ...harnessesQuery(api, workspace),
      staleTime: 0,
    });
    return harnesses.some((version) => version.harness.name === name);
  }

  const mutation = useMutation({
    mutationFn: async (values: HarnessValues) => {
      const harness = toHarness(values);
      if (creating && (await exists(harness.name))) {
        throw new HarnessExists(`A harness named ${harness.name} exists already; edit it instead.`);
      }
      const stored = await unwrap(
        api.PUT("/v1/workspaces/{workspace}/harnesses/{name}", {
          params: { path: { workspace, name: harness.name } },
          body: harness,
        }),
      );
      queryClient.setQueryData(harnessQuery(api, workspace, harness.name).queryKey, stored);
      await queryClient.invalidateQueries({ queryKey: harnessesQuery(api, workspace).queryKey });
      await navigate({
        to: "/w/$workspace/harnesses/$name",
        params: { workspace, name: harness.name },
      });
    },
  });

  // Resolves when the save is done, failed or not, so the form ends its
  // submission either way; the failure is in error.
  async function save(values: HarnessValues) {
    try {
      await mutation.mutateAsync(values);
    } catch {
      // The mutation keeps the error, which the form shows.
    }
  }

  const { error } = mutation;
  return {
    save,
    error:
      error === null
        ? null
        : error instanceof HarnessExists
          ? error.message
          : `The harness could not be saved: ${error.message}`,
  };
}
