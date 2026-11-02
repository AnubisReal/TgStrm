import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from "@headlessui/react";
import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown } from "lucide-react";

export function PillSelect({ value, options, onChange, label, disabled = false }: { value: string; options: { value: string; label: string }[]; onChange: (value: string) => void; label: string; disabled?: boolean }) {
  const root=useRef<HTMLDivElement>(null);
  const [dark,setDark]=useState(false);
  useEffect(()=>{const page=root.current?.closest(".dashboard-page");if(!page)return;const update=()=>setDark(page.classList.contains("dashboard-dark"));update();const observer=new MutationObserver(update);observer.observe(page,{attributes:true,attributeFilter:["class"]});return()=>observer.disconnect()},[]);
  return <Listbox value={value} onChange={onChange} disabled={disabled}><div ref={root} className="library-select">
    <ListboxButton className="season-picker-trigger" aria-label={label}><span>{options.find(option => option.value === value)?.label || label}</span><ChevronDown size={14} aria-hidden="true" /></ListboxButton>
    <ListboxOptions anchor="bottom start" transition modal={false} className={`season-picker-menu library-select-menu${dark ? " season-picker-menu-dark" : ""}`} aria-label={label}>
      {options.map(option => <ListboxOption className="season-picker-option" key={option.value} value={option.value}><Check className="season-picker-check" size={15} aria-hidden="true" /><span>{option.label}</span></ListboxOption>)}
    </ListboxOptions>
  </div></Listbox>;
}
