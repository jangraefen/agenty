import { useId, useRef } from "react";
import { useAppForm } from "@/components/form";
import { Button } from "@/components/ui/button";
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

// HarnessForm authors a harness, previewing it as YAML. A new harness takes
// a name; an existing one keeps its own, as the name is its identity. A
// submit runs the fields' change validators too.
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
  const formElement = useRef<HTMLFormElement>(null);
  const form = useAppForm({
    defaultValues: initial,
    // Submitting checks every field, also when one is already known to be
    // invalid; the form is sent only when all are valid.
    canSubmitWhenInvalid: true,
    onSubmit: ({ value }) => onSubmit(value),
    // Once the errors show, the focus goes to the first field to fix.
    onSubmitInvalid: () => {
      setTimeout(() => {
        formElement.current?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
      });
    },
  });

  return (
    <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]">
      <form
        ref={formElement}
        noValidate
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
      >
        <form.AppField name="name" validators={{ onChange: ({ value }) => validateName(value) }}>
          {(field) => (
            <field.TextField
              label="Name"
              hint={
                creating
                  ? "Lowercase letters, digits and single hyphens."
                  : "A harness keeps its name."
              }
              readOnly={!creating}
            />
          )}
        </form.AppField>
        <form.AppField
          name="instructions"
          validators={{ onChange: ({ value }) => validateRequired(value, "Instructions") }}
        >
          {(field) => (
            <field.TextareaField
              label="Instructions"
              hint="The situation and the goal; the agent decides the steps."
              rows={6}
            />
          )}
        </form.AppField>
        <div className="grid gap-4 sm:grid-cols-2">
          <form.AppField
            name="provider"
            validators={{ onChange: ({ value }) => validateRequired(value, "A provider") }}
          >
            {(field) => <field.TextField label="Provider" />}
          </form.AppField>
          <form.AppField
            name="model"
            validators={{ onChange: ({ value }) => validateRequired(value, "A model") }}
          >
            {(field) => <field.TextField label="Model" />}
          </form.AppField>
        </div>
        <form.AppField name="tools" validators={{ onChange: ({ value }) => validateTools(value) }}>
          {(field) => (
            <field.TextareaField
              label="Granted tools, one per line"
              hint="Named <server>_<tool>. Any other tool call is denied."
              rows={4}
              code
            />
          )}
        </form.AppField>
        <div className="grid gap-4 sm:grid-cols-2">
          <form.AppField
            name="maxSteps"
            validators={{ onChange: ({ value }) => validateLimit(value) }}
          >
            {(field) => <field.NumberField label="Steps at most" />}
          </form.AppField>
          <form.AppField
            name="maxToolCalls"
            validators={{ onChange: ({ value }) => validateLimit(value) }}
          >
            {(field) => <field.NumberField label="Tool calls at most" />}
          </form.AppField>
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
                  <form.AppField
                    name={`policy[${index}].name`}
                    validators={{
                      // A name is a duplicate or not by the others too.
                      onChangeListenTo: policy.state.value
                        .map((_, other) => `policy[${other}].name` as const)
                        .filter((_, other) => other !== index),
                      onChange: ({ fieldApi }) =>
                        validateModuleName(fieldApi.form.getFieldValue("policy"), index),
                    }}
                  >
                    {(field) => <field.TextField label="Module name" />}
                  </form.AppField>
                  <form.AppField
                    name={`policy[${index}].source`}
                    validators={{ onSubmit: ({ value }) => validateRequired(value, "Rego") }}
                  >
                    {(field) => <field.TextareaField label="Rego" rows={6} code />}
                  </form.AppField>
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
                onClick={() => policy.pushValue({ name: "", source: "package agenty.tool\n\n" })}
              >
                Add policy module
              </Button>
            </section>
          )}
        </form.Field>
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
