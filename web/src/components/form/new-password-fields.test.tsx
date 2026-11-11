import { render, screen } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { useAppForm } from "@/components/form/app-form";
import { NewPasswordFields } from "@/components/form/new-password-fields";

interface SignUpValues {
  secret: string;
  again: string;
}

/**
 * A form whose new password fields have other names than those of the group,
 * so that the tests also check that the group maps its fields to the form.
 */
function SignUpForm({ onSubmit }: { onSubmit: (value: SignUpValues) => void }) {
  const form = useAppForm({
    defaultValues: { secret: "", again: "" },
    onSubmit: ({ value }) => {
      onSubmit(value);
    },
  });
  return (
    <form
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <NewPasswordFields
        form={form}
        fields={{ password: "secret", confirmation: "again" }}
        passwordLabel="Password"
        confirmationLabel="Confirm password"
        requiredMessage="Enter a password."
        mismatchMessage="The passwords do not match."
      />
      <button type="submit">Submit</button>
    </form>
  );
}

function renderForm() {
  const onSubmit = vi.fn<(value: SignUpValues) => void>();
  render(<SignUpForm onSubmit={onSubmit} />);
  return { onSubmit };
}

function submit(user: UserEvent) {
  return user.click(screen.getByRole("button", { name: "Submit" }));
}

describe("NewPasswordFields", () => {
  it("given an empty password, when the form is submitted, then the password says that it is required and nothing is submitted", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderForm();

    await submit(user);

    const password = screen.getByLabelText("Password");
    expect(password).toHaveAttribute("aria-invalid", "true");
    expect(password).toHaveAccessibleDescription("Enter a password.");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("given a confirmation that differs from the password, when the form is submitted, then the confirmation says so and nothing is submitted", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderForm();

    await user.type(screen.getByLabelText("Password"), "a long passphrase");
    await user.type(screen.getByLabelText("Confirm password"), "another one");
    await submit(user);

    const confirmation = screen.getByLabelText("Confirm password");
    expect(confirmation).toHaveAttribute("aria-invalid", "true");
    expect(confirmation).toHaveAccessibleDescription(
      "The passwords do not match.",
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("given a mismatch, when the password changes to the confirmation, then the confirmation no longer says so", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.type(screen.getByLabelText("Password"), "a long passphrase");
    await user.type(screen.getByLabelText("Confirm password"), "a long pass");
    await submit(user);

    const password = screen.getByLabelText("Password");
    await user.clear(password);
    await user.type(password, "a long pass");

    const confirmation = screen.getByLabelText("Confirm password");
    expect(confirmation).toHaveAttribute("aria-invalid", "false");
    expect(confirmation).not.toHaveAccessibleDescription();
  });

  it("given the same password twice, when the form is submitted, then the form receives both under its own field names", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderForm();

    await user.type(screen.getByLabelText("Password"), "a long passphrase");
    await user.type(
      screen.getByLabelText("Confirm password"),
      "a long passphrase",
    );
    await submit(user);

    expect(onSubmit).toHaveBeenCalledExactlyOnceWith({
      secret: "a long passphrase",
      again: "a long passphrase",
    });
  });

  it("given the fields, then both are masked, filled in by password managers as a new password, and limited to 128 characters", () => {
    renderForm();

    for (const label of ["Password", "Confirm password"]) {
      const input = screen.getByLabelText(label);
      expect(input).toHaveAttribute("type", "password");
      expect(input).toHaveAttribute("autocomplete", "new-password");
      expect(input).toHaveAttribute("maxlength", "128");
    }
  });
});
