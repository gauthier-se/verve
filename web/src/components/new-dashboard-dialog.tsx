import * as React from "react";
import { useNavigate } from "@tanstack/react-router";
import { useCreateDashboard } from "@/hooks/use-dashboards";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "./ui/dialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

/** NewDashboardDialog creates a dashboard and navigates to it, so the user lands
 *  on the empty grid ready to add panels. */
export function NewDashboardDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [name, setName] = React.useState("");
  const create = useCreateDashboard();
  const navigate = useNavigate();

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim() },
      {
        onSuccess: ({ dashboard }) => {
          setName("");
          onOpenChange(false);
          navigate({ to: "/d/$dashboardId", params: { dashboardId: String(dashboard.id) } });
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>New dashboard</DialogTitle>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="dashboard-name">Name</Label>
            <Input
              id="dashboard-name"
              autoFocus
              placeholder="Training"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending ? "Creating…" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
