import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api/errors";
import type { ApiToken, CreatedApiToken } from "@/api/generated";
import {
  createTokenMutation,
  listTokensQueryKey,
} from "@/api/generated/@tanstack/react-query.gen";
import { CopyField } from "@/components/copy-field";
import { useAppForm } from "@/components/form/app-form";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

type Scope = ApiToken["scopes"][number];

/** Every scope, in the order the dialog lists and sends them. */
const allScopes: Scope[] = ["ingest", "read"];

type Preset = "healthAutoExport" | "readOnly" | "custom";

/** The scopes of the presets that choose them. */
const presetScopes: Record<Exclude<Preset, "custom">, Scope[]> = {
  healthAutoExport: ["ingest"],
  readOnly: ["read"],
};

/** The path of the ingestion endpoint of Health Auto Export. */
const healthAutoExportPath = "/api/v1/ingest/health-auto-export";

/** The longest name of a token, in characters. */
const maxNameLength = 64;

interface CreateTokenValues {
  name: string;
  preset: Preset;
  /** The scopes of the preset Custom. */
  scopes: Scope[];
}

const defaultValues: CreateTokenValues = {
  name: "",
  preset: "healthAutoExport",
  scopes: [],
};

/**
 * The button that opens the creation of a token, and its dialog. The dialog
 * asks for a name and a preset of scopes, then shows the new token once.
 */
export function CreateTokenDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="lg" className="min-h-11 px-4">
          {t("tokens.create.open")}
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-md">
        <CreateToken
          onDone={() => {
            setOpen(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

/**
 * The content of the dialog, which unmounts when the dialog closes. The full
 * token lives only in the result of its mutation: the mutation is never
 * cached once nothing observes it, and the list of tokens is fetched again
 * rather than updated with the answer, so the token leaves memory with the
 * dialog.
 */
function CreateToken({ onDone }: { onDone: () => void }) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const create = useMutation({
    ...createTokenMutation(),
    gcTime: 0,
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: listTokensQueryKey() }),
  });
  const form = useAppForm({
    defaultValues,
    onSubmit: ({ value }) => {
      create.mutate({
        body: {
          name: value.name.trim(),
          scopes:
            value.preset === "custom"
              ? allScopes.filter((scope) => value.scopes.includes(scope))
              : presetScopes[value.preset],
        },
      });
    },
  });

  const presetChoices: {
    preset: Preset;
    label: string;
    description: string;
  }[] = [
    {
      preset: "healthAutoExport",
      label: t("tokens.presets.healthAutoExport.label"),
      description: t("tokens.presets.healthAutoExport.description"),
    },
    {
      preset: "readOnly",
      label: t("tokens.presets.readOnly.label"),
      description: t("tokens.presets.readOnly.description"),
    },
    {
      preset: "custom",
      label: t("tokens.presets.custom.label"),
      description: t("tokens.presets.custom.description"),
    },
  ];

  if (create.data) {
    return (
      <CreatedToken
        token={create.data}
        healthAutoExport={form.getFieldValue("preset") === "healthAutoExport"}
        onDone={onDone}
      />
    );
  }

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <DialogHeader>
        <DialogTitle>{t("tokens.create.title")}</DialogTitle>
        <DialogDescription>{t("tokens.create.description")}</DialogDescription>
      </DialogHeader>
      {create.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(create.error, i18n)}
        </p>
      )}
      <form.AppField
        name="name"
        validators={{
          onChange: ({ value }) => {
            if (value.trim() === "") {
              return t("tokens.create.nameRequired");
            }
            // The server counts characters, not UTF-16 code units.
            return Array.from(value.trim()).length > maxNameLength
              ? t("tokens.create.nameTooLong", { max: maxNameLength })
              : undefined;
          },
        }}
      >
        {(field) => (
          <field.TextField label={t("tokens.create.name")} autoComplete="off" />
        )}
      </form.AppField>
      <form.Field name="preset">
        {(field) => (
          <fieldset role="radiogroup" className="flex flex-col gap-2">
            <legend className="mb-2 text-sm font-medium">
              {t("tokens.create.access")}
            </legend>
            {presetChoices.map(({ preset, label, description }) => (
              <Choice
                key={preset}
                type="radio"
                name={field.name}
                label={label}
                description={description}
                checked={field.state.value === preset}
                onChange={() => {
                  field.handleChange(preset);
                }}
              />
            ))}
          </fieldset>
        )}
      </form.Field>
      <form.Subscribe selector={(state) => state.values.preset}>
        {(preset) =>
          preset === "custom" && (
            <form.Field
              name="scopes"
              validators={{
                onChangeListenTo: ["preset"],
                onChange: ({ value, fieldApi }) =>
                  fieldApi.form.getFieldValue("preset") === "custom" &&
                  value.length === 0
                    ? t("tokens.create.scopesRequired")
                    : undefined,
              }}
            >
              {(field) => (
                <ScopesField
                  name={field.name}
                  value={field.state.value}
                  error={
                    field.state.meta.isTouched
                      ? field.state.meta.errors.find((e) => e !== undefined)
                      : undefined
                  }
                  onChange={(scopes) => {
                    field.handleChange(scopes);
                  }}
                />
              )}
            </form.Field>
          )
        }
      </form.Subscribe>
      <DialogFooter>
        <DialogClose asChild>
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="min-h-11"
          >
            {t("tokens.create.cancel")}
          </Button>
        </DialogClose>
        <Button
          type="submit"
          size="lg"
          className="min-h-11"
          disabled={create.isPending}
        >
          {t("tokens.create.submit")}
        </Button>
      </DialogFooter>
    </form>
  );
}

interface ScopesFieldProps {
  name: string;
  value: Scope[];
  error: string | undefined;
  onChange: (scopes: Scope[]) => void;
}

/** The scopes of a custom token, one checkbox each. */
function ScopesField({ name, value, error, onChange }: ScopesFieldProps) {
  const { t } = useTranslation();
  const errorId = useId();
  // In the order of allScopes.
  const scopeChoices: { scope: Scope; description: string }[] = [
    { scope: "ingest", description: t("tokens.scopes.ingest") },
    { scope: "read", description: t("tokens.scopes.read") },
  ];

  return (
    <fieldset
      className="flex flex-col gap-2"
      aria-describedby={error === undefined ? undefined : errorId}
    >
      <legend className="mb-2 text-sm font-medium">
        {t("tokens.create.scopes")}
      </legend>
      {scopeChoices.map(({ scope, description }) => (
        <Choice
          key={scope}
          type="checkbox"
          name={name}
          label={scope}
          description={description}
          checked={value.includes(scope)}
          onChange={(checked) => {
            onChange(
              checked ? [...value, scope] : value.filter((s) => s !== scope),
            );
          }}
        />
      ))}
      {error !== undefined && (
        <p id={errorId} className="text-sm text-destructive">
          {error}
        </p>
      )}
    </fieldset>
  );
}

interface ChoiceProps {
  type: "radio" | "checkbox";
  name: string;
  label: string;
  description: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}

/** A radio button or a checkbox, with its label and description. */
function Choice({
  type,
  name,
  label,
  description,
  checked,
  onChange,
}: ChoiceProps) {
  const descriptionId = useId();

  return (
    <label className="flex min-h-11 cursor-pointer items-start gap-3 rounded-lg border p-3 has-checked:border-primary has-focus-visible:ring-3 has-focus-visible:ring-ring/50">
      <input
        type={type}
        name={name}
        checked={checked}
        aria-describedby={descriptionId}
        onChange={(event) => {
          onChange(event.target.checked);
        }}
        className="mt-0.5 size-4 shrink-0 accent-primary outline-none"
      />
      <span className="flex flex-col gap-1">
        <span className="font-medium">{label}</span>
        <span id={descriptionId} className="text-muted-foreground">
          {description}
        </span>
      </span>
    </label>
  );
}

interface CreatedTokenProps {
  token: CreatedApiToken;
  /** Whether the token is for Health Auto Export, which shows its setup. */
  healthAutoExport: boolean;
  onDone: () => void;
}

/** The token just created, shown once, with the setup of its app. */
function CreatedToken({ token, healthAutoExport, onDone }: CreatedTokenProps) {
  const { t } = useTranslation();

  return (
    <div className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{t("tokens.created.title")}</DialogTitle>
        <DialogDescription>{t("tokens.created.description")}</DialogDescription>
      </DialogHeader>
      <CopyField
        label={t("tokens.created.token")}
        value={token.token}
        copyLabel={t("tokens.created.copyToken")}
        autoFocus
      />
      {healthAutoExport && <HealthAutoExportSetup token={token.token} />}
      <DialogFooter>
        <Button size="lg" className="min-h-11" onClick={onDone}>
          {t("tokens.created.done")}
        </Button>
      </DialogFooter>
    </div>
  );
}

/**
 * The URL and the header to enter in a REST API automation of Health Auto
 * Export. The URL is the origin of the page, the one that the phone reaches.
 */
function HealthAutoExportSetup({ token }: { token: string }) {
  const { t } = useTranslation();
  const headingId = useId();

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h3 id={headingId} className="font-heading font-medium">
          {t("tokens.healthAutoExport.title")}
        </h3>
        <p className="text-muted-foreground">
          {t("tokens.healthAutoExport.description")}
        </p>
      </div>
      <CopyField
        label={t("tokens.healthAutoExport.url")}
        value={`${window.location.origin}${healthAutoExportPath}`}
        copyLabel={t("tokens.healthAutoExport.copyUrl")}
      />
      <CopyField
        label={t("tokens.healthAutoExport.headerKey")}
        value="Authorization"
        copyLabel={t("tokens.healthAutoExport.copyHeaderKey")}
      />
      <CopyField
        label={t("tokens.healthAutoExport.headerValue")}
        value={`Bearer ${token}`}
        copyLabel={t("tokens.healthAutoExport.copyHeaderValue")}
      />
    </section>
  );
}
