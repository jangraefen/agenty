import type { Metadata } from "next";
import { notFound, redirect } from "next/navigation";
import { Suspense } from "react";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { listMembers } from "@/server/auth/members";
import { ForbiddenError, getCurrentSignedIn, UnauthorizedError } from "@/server/auth/tenant";

export const metadata: Metadata = { title: "Members · Agenty" };

export default function MembersPage() {
  return (
    <Suspense>
      <Members />
    </Suspense>
  );
}

async function signedInAdmin() {
  const signedIn = await getCurrentSignedIn().catch((error: unknown) => {
    if (error instanceof UnauthorizedError) redirect("/sign-in");
    if (error instanceof ForbiddenError) notFound();
    throw error;
  });
  if (signedIn.ctx.role !== "admin") notFound();
  return signedIn;
}

async function Members() {
  const signedIn = await signedInAdmin();
  const members = await listMembers(signedIn.ctx);
  return (
    <>
      <h1 className="font-semibold text-2xl tracking-tight">Members</h1>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Email</TableHead>
            <TableHead>Role</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {members.map((m) => (
            <TableRow key={m.email}>
              <TableCell>{m.name}</TableCell>
              <TableCell>{m.email}</TableCell>
              <TableCell>
                <Badge variant={m.role === "admin" ? "default" : "secondary"}>{m.role}</Badge>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </>
  );
}
