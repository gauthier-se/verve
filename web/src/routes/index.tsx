import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: Index,
});

function Index() {
  // The brand name is not translated.
  return <h1>Verve</h1>;
}
