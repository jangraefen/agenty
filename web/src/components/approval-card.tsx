import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useRouteContext } from "@tanstack/react-router";
import { useId, useState } from "react";
import { ApiError, unwrap } from "@/api/client";
import { approvalsQuery } from "@/api/queries";
import type { components } from "@/api/schema";
import { Json } from "@/components/json";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useNow } from "@/hooks/use-now";
import { formatRemaining, formatTime } from "@/lib/format";

type ApprovalRequest = components["schemas"]["ApprovalRequest"];

// ApprovalCard shows a call waiting for approval, as a list item, and
// answers it as the signed-in user. Everything in the request comes from the
// model or policy, so it is shown as text only.
export function ApprovalCard({
  request,
  workspace,
  showRun,
}: {
  request: ApprovalRequest;
  workspace: string;
  showRun: boolean;
}) {
  const { api } = useRouteContext({ from: "/_authed/w/$workspace" });
  const queryClient = useQueryClient();
  const now = useNow(1000);
  const [reason, setReason] = useState("");
  const id = useId();
  const expired = Date.parse(request.expires_at) <= now;

  const answer = useMutation({
    mutationFn: (approved: boolean) => {
      const why = reason.trim();
      return unwrap(
        api.POST("/v1/workspaces/{workspace}/runs/{id}/approvals/{approval}", {
          params: { path: { workspace, id: request.run_id, approval: request.id } },
          body: why === "" ? { approved } : { approved, reason: why },
        }),
      );
    },
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: approvalsQuery(api, workspace).queryKey }),
  });

  return (
    <li className="grid gap-2 rounded-md border bg-background p-4 text-sm">
      <div className="flex flex-wrap items-baseline gap-2">
        <code className="font-semibold">{request.tool}</code>
        {showRun && (
          <Link
            to="/w/$workspace/runs/$runId"
            params={{ workspace, runId: request.run_id }}
            className="underline"
          >
            run of {request.harness}
          </Link>
        )}
        <span className="ml-auto text-muted-foreground">
          asked <time dateTime={request.created_at}>{formatTime(request.created_at)}</time>,{" "}
          <time dateTime={request.expires_at}>{formatRemaining(request.expires_at, now)}</time>
        </span>
      </div>
      <ul className="list-disc pl-5">
        {request.reasons.map((why) => (
          <li key={why}>{why}</li>
        ))}
      </ul>
      <Json value={request.args} />
      <div className="flex flex-wrap items-end gap-2">
        <div className="grid min-w-60 flex-1 gap-1">
          <label htmlFor={`${id}-reason`} className="text-xs text-muted-foreground">
            Reason (optional)
          </label>
          <Input
            id={`${id}-reason`}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            disabled={expired}
          />
        </div>
        <Button disabled={expired || answer.isPending} onClick={() => answer.mutate(true)}>
          Approve
        </Button>
        <Button
          variant="destructive"
          disabled={expired || answer.isPending}
          onClick={() => answer.mutate(false)}
        >
          Reject
        </Button>
      </div>
      {answer.isError && (
        <p role="alert" className="text-destructive">
          {answer.error instanceof ApiError && answer.error.status === 404
            ? "This request was already answered, or it expired."
            : `The answer could not be sent: ${answer.error.message}`}
        </p>
      )}
    </li>
  );
}
