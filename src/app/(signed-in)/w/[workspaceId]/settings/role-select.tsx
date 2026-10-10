"use client";

import { useEffect, useRef, useState } from "react";
import { useFormStatus } from "react-dom";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type Role = "admin" | "member";

const roleItems = [
  { value: "admin", label: "Admin" },
  { value: "member", label: "Member" },
];

/**
 * A member's role inside the change-role form: choosing a role submits the form at once. While
 * it saves the select is disabled; afterwards it shows `role` from the server again, so a
 * rejected change (e.g. the last admin) goes back to the stored role.
 */
export function RoleSelect({ role, label }: { role: Role; label: string }) {
  const { pending } = useFormStatus();
  const [chosen, setChosen] = useState<string | null>(null);
  const [wasPending, setWasPending] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);

  // Adjusting state while rendering: once a save ends, show the server's role again.
  if (pending !== wasPending) {
    setWasPending(pending);
    if (!pending) setChosen(null);
  }

  // Submits after the commit that put the chosen role into the select's hidden input.
  useEffect(() => {
    if (chosen !== null) trigger.current?.closest("form")?.requestSubmit();
  }, [chosen]);

  return (
    <Select
      disabled={pending}
      items={roleItems}
      name="role"
      onValueChange={(value) => {
        if (typeof value === "string" && value !== (chosen ?? role)) setChosen(value);
      }}
      value={chosen ?? role}
    >
      <SelectTrigger aria-label={label} className="w-28" ref={trigger} size="sm">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {roleItems.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
