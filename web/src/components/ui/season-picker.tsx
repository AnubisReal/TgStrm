import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from "@headlessui/react";
import { Check, ChevronDown } from "lucide-react";

export function SeasonPicker({ value, seasons, onChange, dark = false }: { value: string; seasons: number[]; onChange: (value: string) => void; dark?: boolean }) {
  const options = [{ value: "all", label: "Todas las temporadas" }, ...seasons.map(season => ({ value: String(season), label: season === 0 ? "Especiales" : `Temporada ${season}` }))];
  return <Listbox value={value} onChange={onChange}>
    {({ open }) => <div className={`season-picker${open ? " is-open" : ""}`}>
      <div className="season-picker-surface">
        <ListboxButton className="season-picker-trigger" aria-label="Filtrar por temporada"><span>{options.find(option => option.value === value)?.label || "Todas las temporadas"}</span><ChevronDown size={14} aria-hidden="true" /></ListboxButton>
        <ListboxOptions anchor="bottom start" transition className={`season-picker-menu${dark ? " season-picker-menu-dark" : ""}`} aria-label="Temporadas" modal={false}>
          {options.map(option => <ListboxOption className="season-picker-option" key={option.value} value={option.value}><Check className="season-picker-check" size={16} aria-hidden="true" /><span>{option.label}</span></ListboxOption>)}
        </ListboxOptions>
      </div>
    </div>}
  </Listbox>;
}
