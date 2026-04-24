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
  /** Container widgets accept dropped children that render in document flow. */
  isContainer?: boolean;
}

/** Layout fields offered for any widget when it lives inside a container.
 * They control how the child participates in flex layout. Stored alongside
 * the widget's regular props. */
export const childLayoutFields: PropField[] = [
  { key: '$grow', label: 'Grow', type: 'number', step: 1, placeholder: '0 = fixed' },
  { key: '$basis', label: 'Basis', type: 'text', placeholder: 'e.g. auto, 200px, 50%' },
  { key: '$alignSelf', label: 'Align self', type: 'select', options: ['', 'start', 'center', 'end', 'stretch'] },
];

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
    type: 'stack',
    label: 'Stack',
    defaultSize: { w: 480, h: 240 },
    defaultProps: {
      direction: 'column',
      gap: 8,
      padding: 8,
      align: 'stretch',
      justify: 'start',
      wrap: 'no',
      width: 'auto',
      height: 'auto',
    },
    propFields: [
      { key: 'direction', label: 'Direction', type: 'select', options: ['column', 'row'] },
      { key: 'gap', label: 'Gap (px)', type: 'number', step: 1 },
      { key: 'padding', label: 'Padding (px)', type: 'number', step: 1 },
      { key: 'align', label: 'Align (cross)', type: 'select', options: ['start', 'center', 'end', 'stretch'] },
      { key: 'justify', label: 'Justify (main)', type: 'select', options: ['start', 'center', 'end', 'space-between', 'space-around', 'space-evenly'] },
      { key: 'wrap', label: 'Wrap', type: 'select', options: ['no', 'yes'] },
      { key: 'width', label: 'Width', type: 'text', placeholder: 'auto, 100%, 320px' },
      { key: 'height', label: 'Height', type: 'text', placeholder: 'auto, 100%, 240px' },
    ],
    bindingSlots: [],
    isContainer: true,
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
  const w: HmiWidget = {
    id: newWidgetId(existingIds),
    type,
    x,
    y,
    w: size.w,
    h: size.h,
    props: { ...(schema?.defaultProps ?? {}) },
    bindings: {},
  };
  if (schema?.isContainer) w.children = [];
  return w;
}

/** Recursively walk a widget tree and yield each widget. */
export function* walkWidgets(widgets: HmiWidget[]): Generator<HmiWidget> {
  for (const w of widgets) {
    yield w;
    if (w.children?.length) yield* walkWidgets(w.children);
  }
}

export function findWidget(widgets: HmiWidget[], id: string): HmiWidget | undefined {
  for (const w of walkWidgets(widgets)) if (w.id === id) return w;
  return undefined;
}

/** Map every widget in a tree (top-level + children recursive). Returns a new
 * array; widgets are kept as-is unless `fn` returns a different reference. */
export function mapWidgets(widgets: HmiWidget[], fn: (w: HmiWidget) => HmiWidget): HmiWidget[] {
  return widgets.map((w) => {
    const next = fn(w);
    if (next.children?.length) {
      const mappedChildren = mapWidgets(next.children, fn);
      if (mappedChildren !== next.children) return { ...next, children: mappedChildren };
    }
    return next;
  });
}

/** Remove a widget by id from a tree (top-level or any descendant). */
export function removeWidget(widgets: HmiWidget[], id: string): HmiWidget[] {
  const out: HmiWidget[] = [];
  for (const w of widgets) {
    if (w.id === id) continue;
    if (w.children?.length) {
      const nextChildren = removeWidget(w.children, id);
      if (nextChildren !== w.children) {
        out.push({ ...w, children: nextChildren });
        continue;
      }
    }
    out.push(w);
  }
  return out;
}

/** Append a widget into the container with `parentId`, or top-level if null. */
export function appendChild(widgets: HmiWidget[], parentId: string | null, child: HmiWidget): HmiWidget[] {
  if (parentId === null) return [...widgets, child];
  return mapWidgets(widgets, (w) =>
    w.id === parentId ? { ...w, children: [...(w.children ?? []), child] } : w
  );
}

/** Replace a widget in the tree by id (preserving its position). */
export function replaceWidget(widgets: HmiWidget[], updated: HmiWidget): HmiWidget[] {
  return mapWidgets(widgets, (w) => (w.id === updated.id ? updated : w));
}

/** Find the parent widget of `id` (the one whose `children` contains it).
 * Returns undefined if `id` is top-level or not found. */
export function findParent(widgets: HmiWidget[], id: string): HmiWidget | undefined {
  for (const w of widgets) {
    if (w.children?.some((c) => c.id === id)) return w;
    if (w.children?.length) {
      const found = findParent(w.children, id);
      if (found) return found;
    }
  }
  return undefined;
}

/** Collect all existing widget ids in the tree (used when generating a new id). */
export function collectIds(widgets: HmiWidget[]): string[] {
  const ids: string[] = [];
  for (const w of walkWidgets(widgets)) ids.push(w.id);
  return ids;
}
