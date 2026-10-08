import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { useId, useState } from "react";
import { ApiError, type Schemas, unwrap } from "@/api/client";
import { approvalsQuery, recentChatsKey } from "@/api/queries";
import type { AnswerOutcome } from "@/components/answer-notice";
import { Json } from "@/components/json";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useNow } from "@/hooks/use-now";
import { formatRemaining, formatTime } from "@/lib/format";

type ApprovalRequest = Schemas["ApprovalRequest"];

// ApprovalCard shows a call waiting for approval, as a list item, and
// answers it as the signed-in user, telling onOutcome how that went: the card
// itself leaves when the request does. Everything in the request comes from
// the model or policy, so it is shown as text only.
export function ApprovalCard({
  request,
  workspace,
  onOutcome,
}: {
  request: ApprovalRequest;
  workspace: string;
  onOutcome: (outcome: AnswerOutcome) => void;
}) {
  const { api } = useRouteContext({ from: "/_authed" });
  const queryClient = useQueryClient();
  const now = useNow(1000);
  const [reason, setReason] = useState("");
  const id = useId();
  const expired = Date.parse(request.expires_at) <= now;
  const { tool } = request;

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
    onSuccess: (_, approved) =>
      onOutcome({ ok: true, message: `${approved ? "Approved" : "Rejected"} ${tool}.` }),
    onError: (error) =>
      onOutcome({
        ok: false,
        message:
          error instanceof ApiError && error.status === 404
            ? `${tool} was already answered, or it expired: your answer did not count.`
            : `The answer to ${tool} could not be sent: ${error.message}`,
      }),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: approvalsQuery(api, workspace).queryKey });
      // An answered run waits no longer.
      void queryClient.invalidateQueries({ queryKey: recentChatsKey() });
    },
  });
  const blocked = expired || answer.isPending;

  return (
    <li className="grid gap-2 rounded-md border bg-background p-4 text-sm">
      <div className="flex flex-wrap items-baseline gap-2">
        <code id={`${id}-tool`} className="font-semibold">
          {tool}
        </code>
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
        <div className="grid min-w-0 grow basis-60 gap-1">
          <Label htmlFor={`${id}-reason`} className="text-xs font-normal text-muted-foreground">
            Reason (optional)
          </Label>
          <Input
            id={`${id}-reason`}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            readOnly={expired}
          />
        </div>
        <Button
          aria-disabled={blocked}
          aria-describedby={`${id}-tool`}
          onClick={() => answer.mutate(true)}
        >
          Approve
        </Button>
        <Button
          variant="destructive"
          aria-disabled={blocked}
          aria-describedby={`${id}-tool`}
          onClick={() => answer.mutate(false)}
        >
          Reject
        </Button>
      </div>
    </li>
  );
}
