import { withFieldGroup } from "@/components/form/app-form";

/**
 * The fields where a user chooses a new password: the password, which is
 * required, and its confirmation, which must match it. Both are masked and
 * filled in by password managers as a new password. `fields` maps them to the
 * fields of the form, and the page gives their translated text.
 */
export const NewPasswordFields = withFieldGroup({
  defaultValues: { password: "", confirmation: "" },
  props: {
    /** The label of the password. */
    passwordLabel: "",
    /** The label of the confirmation. */
    confirmationLabel: "",
    /** The error of an empty password. */
    requiredMessage: "",
    /** The error of a confirmation that differs from the password. */
    mismatchMessage: "",
  },
  render: function Render({
    group,
    passwordLabel,
    confirmationLabel,
    requiredMessage,
    mismatchMessage,
  }) {
    return (
      <>
        <group.AppField
          name="password"
          validators={{
            onChange: ({ value }) =>
              value === "" ? requiredMessage : undefined,
          }}
        >
          {(field) => (
            <field.TextField
              label={passwordLabel}
              type="password"
              autoComplete="new-password"
              maxLength={128}
            />
          )}
        </group.AppField>
        <group.AppField
          name="confirmation"
          validators={{
            onChangeListenTo: ["password"],
            onChange: ({ value }) =>
              value === group.getFieldValue("password")
                ? undefined
                : mismatchMessage,
          }}
        >
          {(field) => (
            <field.TextField
              label={confirmationLabel}
              type="password"
              autoComplete="new-password"
              maxLength={128}
            />
          )}
        </group.AppField>
      </>
    );
  },
});
