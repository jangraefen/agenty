import { useForm } from "@tanstack/react-form";
import { type ReactNode, useId } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  type HarnessValues,
  toHarness,
  validateLimit,
  validateModuleName,
  validateName,
  validateRequired,
  validateTools,
} from "@/lib/harness-form";
import { harnessYaml } from "@/lib/harness-yaml";
import { cn } from "@/lib/utils";

const textareaClass =
  "w-full rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring aria-invalid:border-destructive aria-invalid:ring-destructive";

// Field lays out one labelled control with its hint and its error, which it
// ties to the control for assistive technology.
function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  error: string | undefined;
  children: (props: {
    id: string;
    "aria-invalid": boolean;
    "aria-describedby": string | undefined;
  }) => ReactNode;
}) {
  const described = [
    hint === undefined ? null : `${id}-hint`,
    error === undefined ? null : `${id}-error`,
  ]
    .filter((part) => part !== null)
    .join(" ");
  return (
    <div className="grid gap-1">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
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

// HarnessForm authors a harness, previewing it as YAML. A new harness takes
// a name; an existing one keeps its own, as the name is its identity.
export function HarnessForm({
  initial,
  creating,
  submitLabel,
  error,
  onSubmit,
}: {
  initial: HarnessValues;
  creating: boolean;
  submitLabel: string;
  error: string | null;
  onSubmit: (values: HarnessValues) => Promise<void>;
}) {
  const id = useId();
  const form = useForm({
    defaultValues: initial,
    // Submitting checks every field, also when one is already known to be
    // invalid; the form is sent only when all are valid.
    canSubmitWhenInvalid: true,
    onSubmit: ({ value }) => onSubmit(value),
  });

  // A field's error shows once it was left or a submit was tried.
  function shown(meta: { isTouched: boolean; errors: unknown[] }, attempts: number) {
    const [first] = meta.errors;
    return (meta.isTouched || attempts > 0) && typeof first === "string" ? first : undefined;
  }

  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
      <form
        noValidate
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
      >
        <form.Subscribe selector={(state) => state.submissionAttempts}>
          {(attempts) => (
            <>
              <form.Field
                name="name"
                validators={{
                  onChange: ({ value }) => validateName(value),
                  onSubmit: ({ value }) => validateName(value),
                }}
              >
                {(field) => (
                  <Field
                    id={`${id}-name`}
                    label="Name"
                    hint={
                      creating
                        ? "Lowercase letters, digits and single hyphens."
                        : "A harness keeps its name."
                    }
                    error={shown(field.state.meta, attempts)}
                  >
                    {(props) => (
                      <Input
                        {...props}
                        readOnly={!creating}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(event) => field.handleChange(event.target.value)}
                      />
                    )}
                  </Field>
                )}
              </form.Field>
              <form.Field
                name="instructions"
                validators={{
                  onChange: ({ value }) => validateRequired(value, "Instructions"),
                  onSubmit: ({ value }) => validateRequired(value, "Instructions"),
                }}
              >
                {(field) => (
                  <Field
                    id={`${id}-instructions`}
                    label="Instructions"
                    hint="The situation and the goal; the agent decides the steps."
                    error={shown(field.state.meta, attempts)}
                  >
                    {(props) => (
                      <textarea
                        {...props}
                        rows={6}
                        className={textareaClass}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(event) => field.handleChange(event.target.value)}
                      />
                    )}
                  </Field>
                )}
              </form.Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <form.Field
                  name="provider"
                  validators={{
                    onChange: ({ value }) => validateRequired(value, "A provider"),
                    onSubmit: ({ value }) => validateRequired(value, "A provider"),
                  }}
                >
                  {(field) => (
                    <Field
                      id={`${id}-provider`}
                      label="Provider"
                      error={shown(field.state.meta, attempts)}
                    >
                      {(props) => (
                        <Input
                          {...props}
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(event.target.value)}
                        />
                      )}
                    </Field>
                  )}
                </form.Field>
                <form.Field
                  name="model"
                  validators={{
                    onChange: ({ value }) => validateRequired(value, "A model"),
                    onSubmit: ({ value }) => validateRequired(value, "A model"),
                  }}
                >
                  {(field) => (
                    <Field
                      id={`${id}-model`}
                      label="Model"
                      error={shown(field.state.meta, attempts)}
                    >
                      {(props) => (
                        <Input
                          {...props}
                          value={field.state.value}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(event.target.value)}
                        />
                      )}
                    </Field>
                  )}
                </form.Field>
              </div>
              <form.Field
                name="tools"
                validators={{
                  onChange: ({ value }) => validateTools(value),
                  onSubmit: ({ value }) => validateTools(value),
                }}
              >
                {(field) => (
                  <Field
                    id={`${id}-tools`}
                    label="Granted tools, one per line"
                    hint="Named <server>_<tool>. Any other tool call is denied."
                    error={shown(field.state.meta, attempts)}
                  >
                    {(props) => (
                      <textarea
                        {...props}
                        rows={4}
                        className={cn(textareaClass, "font-mono")}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(event) => field.handleChange(event.target.value)}
                      />
                    )}
                  </Field>
                )}
              </form.Field>
              <div className="grid gap-4 sm:grid-cols-2">
                {(["maxSteps", "maxToolCalls"] as const).map((name) => (
                  <form.Field
                    key={name}
                    name={name}
                    validators={{
                      onChange: ({ value }) => validateLimit(value),
                      onSubmit: ({ value }) => validateLimit(value),
                    }}
                  >
                    {(field) => (
                      <Field
                        id={`${id}-${name}`}
                        label={name === "maxSteps" ? "Steps at most" : "Tool calls at most"}
                        error={shown(field.state.meta, attempts)}
                      >
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
                    )}
                  </form.Field>
                ))}
              </div>
              <form.Field name="policy" mode="array">
                {(policy) => (
                  <section className="grid gap-3">
                    <h2 className="text-sm font-medium">Policy</h2>
                    <p className="text-xs text-muted-foreground">
                      Rego modules in package agenty.tool that tighten the central policy for this
                      harness, with deny and require_approval rules.
                    </p>
                    {policy.state.value.map((_, index) => (
                      <fieldset
                        // Modules have no id; their order only changes by removal.
                        // biome-ignore lint/suspicious/noArrayIndexKey: see above.
                        key={index}
                        className="grid gap-3 rounded-md border p-3"
                      >
                        <legend className="px-1 text-sm">Policy module {index + 1}</legend>
                        <form.Field
                          name={`policy[${index}].name`}
                          validators={{
                            onChange: ({ fieldApi }) =>
                              validateModuleName(fieldApi.form.getFieldValue("policy"), index),
                            onSubmit: ({ fieldApi }) =>
                              validateModuleName(fieldApi.form.getFieldValue("policy"), index),
                          }}
                        >
                          {(field) => (
                            <Field
                              id={`${id}-policy-${index}-name`}
                              label="Module name"
                              error={shown(field.state.meta, attempts)}
                            >
                              {(props) => (
                                <Input
                                  {...props}
                                  value={field.state.value}
                                  onBlur={field.handleBlur}
                                  onChange={(event) => field.handleChange(event.target.value)}
                                />
                              )}
                            </Field>
                          )}
                        </form.Field>
                        <form.Field
                          name={`policy[${index}].source`}
                          validators={{
                            onSubmit: ({ value }) => validateRequired(value, "Rego"),
                          }}
                        >
                          {(field) => (
                            <Field
                              id={`${id}-policy-${index}-source`}
                              label="Rego"
                              error={shown(field.state.meta, attempts)}
                            >
                              {(props) => (
                                <textarea
                                  {...props}
                                  rows={6}
                                  spellCheck={false}
                                  className={cn(textareaClass, "font-mono")}
                                  value={field.state.value}
                                  onBlur={field.handleBlur}
                                  onChange={(event) => field.handleChange(event.target.value)}
                                />
                              )}
                            </Field>
                          )}
                        </form.Field>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="justify-self-start"
                          aria-label={`Remove policy module ${index + 1}`}
                          onClick={() => policy.removeValue(index)}
                        >
                          Remove
                        </Button>
                      </fieldset>
                    ))}
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="justify-self-start"
                      onClick={() =>
                        policy.pushValue({ name: "", source: "package agenty.tool\n\n" })
                      }
                    >
                      Add policy module
                    </Button>
                  </section>
                )}
              </form.Field>
            </>
          )}
        </form.Subscribe>
        {error !== null && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <form.Subscribe selector={(state) => state.isSubmitting}>
          {(submitting) => (
            // Not disabled, which would drop the focus.
            <Button type="submit" className="justify-self-start" aria-disabled={submitting}>
              {submitLabel}
            </Button>
          )}
        </form.Subscribe>
      </form>
      <aside>
        <h2 id={`${id}-preview`} className="text-sm font-medium">
          YAML preview
        </h2>
        <form.Subscribe selector={(state) => state.values}>
          {(values) => (
            <figure aria-labelledby={`${id}-preview`} className="mt-2">
              <pre className="rounded-md bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-words">
                {harnessYaml(toHarness(values))}
              </pre>
            </figure>
          )}
        </form.Subscribe>
      </aside>
    </div>
  );
}
