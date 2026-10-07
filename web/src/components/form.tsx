import { createFormHook, createFormHookContexts } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

// The app's form hook, whose fields bring their label, hint and error.

const { fieldContext, formContext, useFieldContext } = createFormHookContexts();

interface ControlProps {
  id: string;
  "aria-invalid": boolean;
  "aria-describedby": string | undefined;
}

// Field lays out one labelled control with its hint and its error, which it
// ties to the control for assistive technology. The error shows once the
// field was changed or left, or a submit was tried, which touches them all.
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

export const { useAppForm } = createFormHook({
  fieldContext,
  formContext,
  fieldComponents: { TextField, TextareaField, NumberField },
  formComponents: {},
});
