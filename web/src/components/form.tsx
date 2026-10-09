import { createFormHook, createFormHookContexts } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

/**
 * The app's form hook, whose fields bring their label, hint and error.
 *
 * TanStack Form's createFormHook binds field components to a form: a form
 * made with useAppForm renders `form.AppField`, whose child gets the field's
 * state and the components below as `field.TextField`, `field.TextareaField`
 * and `field.NumberField`. Each reads its field from context
 * (useFieldContext), so a form names a field once and the component does the
 * wiring: value, change, blur, and the error's accessible description.
 * HarnessForm is its user.
 */

const { fieldContext, formContext, useFieldContext } = createFormHookContexts();

/** What Field gives its control: the id its label points to, and the error and hint links. */
interface ControlProps {
  id: string;
  "aria-invalid": boolean;
  "aria-describedby": string | undefined;
}

/**
 * Field lays out one labelled control with its hint and its error, which it
 * ties to the control for assistive technology. The error shows once the
 * field was changed or left, or a submit was tried, which touches them all.
 *
 * It renders the control through a render prop, so each field component
 * picks its own control while the label, ids and messages stay in one place.
 * Only the first of a field's errors shows, so the user fixes one at a time.
 */
function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string | undefined;
  children: (props: ControlProps) => ReactNode;
}) {
  const field = useFieldContext<unknown>();
  const id = useId();
  const [first] = field.state.meta.errors;
  const error = field.state.meta.isTouched && typeof first === "string" ? first : undefined;
  const described = [
    hint === undefined ? null : `${id}-hint`,
    error === undefined ? null : `${id}-error`,
  ]
    .filter((part) => part !== null)
    .join(" ");
  return (
    <div className="grid gap-1">
      <Label htmlFor={id}>{label}</Label>
      {children({
        id,
        "aria-invalid": error !== undefined,
        "aria-describedby": described === "" ? undefined : described,
      })}
      {hint !== undefined && (
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      {error !== undefined && (
        <p id={`${id}-error`} className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

/** A single-line text field; read-only where the value may not change, as an existing harness's name. */
function TextField({
  label,
  hint,
  readOnly,
}: {
  label: string;
  hint?: string;
  readOnly?: boolean;
}) {
  const field = useFieldContext<string>();
  return (
    <Field label={label} hint={hint}>
      {(props) => (
        <Input
          {...props}
          readOnly={readOnly}
          value={field.state.value}
          onBlur={field.handleBlur}
          onChange={(event) => field.handleChange(event.target.value)}
        />
      )}
    </Field>
  );
}

/** A multi-line text field, for instructions, tool lists and Rego. */
function TextareaField({
  label,
  hint,
  rows,
  code = false,
}: {
  label: string;
  hint?: string;
  rows: number;
  /** Whether it holds code, shown monospaced and not spellchecked. */
  code?: boolean;
}) {
  const field = useFieldContext<string>();
  return (
    <Field label={label} hint={hint}>
      {(props) => (
        <Textarea
          {...props}
          rows={rows}
          spellCheck={!code}
          className={code ? "font-mono" : undefined}
          value={field.state.value}
          onBlur={field.handleBlur}
          onChange={(event) => field.handleChange(event.target.value)}
        />
      )}
    </Field>
  );
}

/**
 * A field for a whole number of at least 1, such as a limit. A cleared or
 * unparsable input holds NaN, the value the validator refuses, and shows as
 * empty rather than as "NaN".
 */
function NumberField({ label }: { label: string }) {
  const field = useFieldContext<number>();
  return (
    <Field label={label}>
      {(props) => (
        <Input
          {...props}
          type="number"
          min={1}
          step={1}
          value={Number.isNaN(field.state.value) ? "" : field.state.value}
          onBlur={field.handleBlur}
          onChange={(event) => field.handleChange(event.target.valueAsNumber)}
        />
      )}
    </Field>
  );
}

/** The form hook of the app's forms, with its field components bound to it. */
export const { useAppForm } = createFormHook({
  fieldContext,
  formContext,
  fieldComponents: { TextField, TextareaField, NumberField },
  formComponents: {},
});
