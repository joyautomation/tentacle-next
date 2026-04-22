import type { HmiWidget } from '$lib/types/hmi';

export type FieldType = 'text' | 'number' | 'select' | 'component';

export interface PropField {
  key: string;
  label: string;
  type: FieldType;
  options?: string[];
  step?: number;
  placeholder?: string;
}

export interface BindingSlot {
  key: string;
  label: string;
}

export interface WidgetSchema {
  type: HmiWidget['type'];
  label: string;
  defaultSize: { w: number; h: number };
  defaultProps: Record<string, unknown>;
  propFields: PropField[];
  bindingSlots: BindingSlot[];
}

export const widgetSchemas: WidgetSchema[] = [
  {
    type: 'label',
    label: 'Label',
    defaultSize: { w: 200, h: 40 },
    defaultProps: { text: 'Label', size: 'md', weight: 'normal', align: 'left' },
    propFields: [
      { key: 'text', label: 'Text', type: 'text' },
      { key: 'size', label: 'Size', type: 'select', options: ['sm', 'md', 'lg', 'xl'] },
      { key: 'weight', label: 'Weight', type: 'select', options: ['normal', 'bold'] },
      { key: 'align', label: 'Align', type: 'select', options: ['left', 'center', 'right'] },
    ],
    bindingSlots: [],
  },
  {
    type: 'numeric',
    label: 'Numeric',
    defaultSize: { w: 220, h: 96 },
    defaultProps: { label: 'Value', precision: 2, units: '' },
    propFields: [
      { key: 'label', label: 'Label', type: 'text' },
      { key: 'precision', label: 'Precision', type: 'number', step: 1 },
      { key: 'units', label: 'Units', type: 'text', placeholder: 'e.g. °C' },
    ],
    bindingSlots: [{ key: 'value', label: 'Value' }],
  },
  {
    type: 'indicator',
    label: 'Indicator',
    defaultSize: { w: 220, h: 48 },
    defaultProps: { label: 'Indicator' },
    propFields: [{ key: 'label', label: 'Label', type: 'text' }],
    bindingSlots: [{ key: 'value', label: 'Value' }],
  },
  {
    type: 'bar',
    label: 'Bar',
    defaultSize: { w: 320, h: 88 },
    defaultProps: { label: 'Bar', min: 0, max: 100 },
    propFields: [
      { key: 'label', label: 'Label', type: 'text' },
      { key: 'min', label: 'Min', type: 'number' },
      { key: 'max', label: 'Max', type: 'number' },
    ],
    bindingSlots: [{ key: 'value', label: 'Value' }],
  },
  {
    type: 'componentInstance',
    label: 'Component',
    defaultSize: { w: 320, h: 200 },
    defaultProps: { componentId: '' },
    propFields: [
      { key: 'componentId', label: 'Component', type: 'component' },
    ],
    // `root` binds the UDT instance whose members the component's
    // udtMember bindings resolve against at runtime.
    bindingSlots: [{ key: 'root', label: 'UDT instance' }],
  },
];

export const schemaByType: Record<string, WidgetSchema> = Object.fromEntries(
  widgetSchemas.map((s) => [s.type, s]),
);

let nextId = 1;
export function newWidgetId(existing: string[]): string {
  const used = new Set(existing);
  while (used.has(`w${nextId}`)) nextId++;
  const id = `w${nextId}`;
  nextId++;
  return id;
}

export function makeWidget(type: HmiWidget['type'], x: number, y: number, existingIds: string[]): HmiWidget {
  const schema = schemaByType[type];
  const size = schema?.defaultSize ?? { w: 160, h: 80 };
  return {
    id: newWidgetId(existingIds),
    type,
    x,
    y,
    w: size.w,
    h: size.h,
    props: { ...(schema?.defaultProps ?? {}) },
    bindings: {},
  };
}
