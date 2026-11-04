import type { InputHTMLAttributes, Ref } from "react";

interface Props extends InputHTMLAttributes<HTMLInputElement> {
  label: string;
  error?: string;
  ref?: Ref<HTMLInputElement>;
}

export default function Field({ label, error, ...input }: Props) {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span className="font-medium text-gray-700">{label}</span>
      <input
        {...input}
        className={`rounded border px-3 py-2 outline-none focus:ring-2 focus:ring-gray-900/20 ${
          error ? "border-red-500" : "border-gray-300"
        }`}
      />
      {error && <span className="text-xs text-red-600">{error}</span>}
    </label>
  );
}
