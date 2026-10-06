/** How answering an approval request went, to tell the user. */
export interface AnswerOutcome {
  ok: boolean;
  message: string;
}

// AnswerNotice tells how the last answer went. It belongs to the page, not
// to the answered request's card: once answered, the request leaves the
// list, and its card with it, whether the answer counted or not.
export function AnswerNotice({ outcome }: { outcome: AnswerOutcome | null }) {
  return (
    <>
      <p role="status" aria-label="Answers" className="text-sm">
        {outcome?.ok === true ? outcome.message : ""}
      </p>
      {outcome?.ok === false && (
        <p role="alert" className="text-sm text-destructive">
          {outcome.message}
        </p>
      )}
    </>
  );
}
