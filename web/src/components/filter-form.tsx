/**
 * The filters of the audit log's views (routes/_authed/audit/index.tsx and
 * events.tsx), which keep them in the URL's search parameters.
 *
 * A view validates its search with textFilters, and renders FilterForm with
 * those values; submitting hands the form's values to the view, which
 * navigates to them, and so validates them again on the way back in.
 */
import { type FormEvent, type ReactNode, useId } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

/**
 * textFilters returns the named text filters of a search, trimmed, leaving
 * out empty ones and anything else: the router passes on the search
 * parameters no route validates, too. Leaving out empty ones keeps the URL
 * and the query key free of filters that filter nothing.
 */
export function textFilters<Name extends string>(
  search: Record<string, unknown>,
  names: readonly Name[],
): Partial<Record<Name, string>> {
  const out: Partial<Record<Name, string>> = {};
  for (const name of names) {
    const value = search[name];
    if (typeof value === "string" && value.trim() !== "") {
      out[name] = value.trim();
    }
  }
  return out;
}

/**
 * FilterForm is a row of labelled text filters, and children for others,
 * which filters on submit with the form's values. Keyed by its values, it
 * shows them after each search.
 *
 * Its inputs are uncontrolled, taking the values only as their defaults,
 * so the key is what resets them when the URL changes, as on Back.
 * children gets the form's id prefix, for its own controls' labels.
 */
export function FilterForm<Name extends string>({
  fields,
  values,
  onFilter,
  children,
}: {
  fields: readonly { name: Name; label: string }[];
  values: Partial<Record<Name, string>>;
  onFilter: (form: Record<string, unknown>) => void;
  children?: (id: string) => ReactNode;
}) {
  const id = useId();
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    onFilter(Object.fromEntries(new FormData(event.currentTarget)));
  }
  return (
    <form key={JSON.stringify(values)} onSubmit={submit} className="flex flex-wrap items-end gap-4">
      {fields.map(({ name, label }) => (
        <div key={name} className="grid gap-1">
          <Label htmlFor={`${id}-${name}`} className="font-normal">
            {label}
          </Label>
          <Input
            id={`${id}-${name}`}
            name={name}
            defaultValue={values[name] ?? ""}
            className="h-8 w-40"
          />
        </div>
      ))}
      {children?.(id)}
      <Button type="submit" variant="outline" size="sm">
        Filter
      </Button>
    </form>
  );
}
