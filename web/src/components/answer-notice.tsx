/**
 * How the last answer to an approval request went, told on the chat's page.
 * ApprovalCard reports an outcome; Chat (components/chat.tsx) keeps the
 * latest and shows it with AnswerNotice.
 */

/**
 * How answering an approval request went, to tell the user: ok with a
 * confirmation, or not with why the answer did not count.
 */
export interface AnswerOutcome {
  ok: boolean;
  message: string;
}

/**
 * AnswerNotice tells how the last answer went. It belongs to the page, not
 * to the answered request's card: once answered, the request leaves the
 * list, and its card with it, whether the answer counted or not.
 *
 * A confirmation goes into a status region that is always rendered, so
 * screen readers announce its change politely; a failure is an alert.
 */
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
