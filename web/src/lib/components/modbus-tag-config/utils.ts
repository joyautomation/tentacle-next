export const FUNCTION_CODES = ['holding', 'input', 'coil', 'discrete'] as const;
export type FunctionCode = (typeof FUNCTION_CODES)[number];

export const MODBUS_DATATYPES = [
  'boolean',
  'int16',
  'uint16',
  'int32',
  'uint32',
  'float32',
  'float64',
] as const;
export type ModbusDatatype = (typeof MODBUS_DATATYPES)[number];

export const BYTE_ORDERS = ['ABCD', 'BADC', 'CDAB', 'DCBA'] as const;
export type ByteOrder = (typeof BYTE_ORDERS)[number];

export function registerWidth(dt: string): number {
  switch (dt) {
    case 'boolean':
    case 'int16':
    case 'uint16':
      return 1;
    case 'int32':
    case 'uint32':
    case 'float32':
      return 2;
    case 'float64':
      return 4;
    default:
      return 1;
  }
}

export function functionCodeToInt(fc: string): number {
  switch (fc) {
    case 'coil':
      return 1;
    case 'discrete':
      return 2;
    case 'holding':
      return 3;
    case 'input':
      return 4;
    default:
      return 3;
  }
}

export function intToFunctionCode(n: number): FunctionCode {
  switch (n) {
    case 1:
      return 'coil';
    case 2:
      return 'discrete';
    case 3:
      return 'holding';
    case 4:
      return 'input';
    default:
      return 'holding';
  }
}

export function gatewayDatatype(modbusDatatype: string): string {
  return modbusDatatype === 'boolean' ? 'boolean' : 'number';
}

export interface ParsedCsvTag {
  name: string;
  address: number;
  functionCode: FunctionCode;
  datatype: ModbusDatatype;
  byteOrder: string;
  description: string;
}

export function parseCsv(text: string): { tags: ParsedCsvTag[]; errors: string[] } {
  const lines = text.trim().split('\n');
  if (lines.length === 0) return { tags: [], errors: ['Empty CSV'] };

  const tags: ParsedCsvTag[] = [];
  const errors: string[] = [];

  // Detect header
  let startLine = 0;
  const firstLine = lines[0].toLowerCase();
  if (firstLine.includes('name') || firstLine.includes('tag') || firstLine.includes('address')) {
    startLine = 1;
  }

  for (let i = startLine; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line) continue;

    const cols = line.split(',').map((c) => c.trim());
    if (cols.length < 2) {
      errors.push(`Line ${i + 1}: need at least name and address`);
      continue;
    }

    const name = cols[0];
    const address = parseInt(cols[1], 10);
    if (isNaN(address)) {
      errors.push(`Line ${i + 1}: invalid address "${cols[1]}"`);
      continue;
    }

    const fc = (cols[2] || 'holding').toLowerCase() as FunctionCode;
    if (!FUNCTION_CODES.includes(fc)) {
      errors.push(`Line ${i + 1}: invalid function code "${cols[2]}"`);
      continue;
    }

    const dt = (cols[3] || 'uint16') as ModbusDatatype;
    if (!MODBUS_DATATYPES.includes(dt)) {
      errors.push(`Line ${i + 1}: invalid datatype "${cols[3]}"`);
      continue;
    }

    const byteOrder = cols[4] || '';
    const description = cols[5] || '';

    tags.push({ name, address, functionCode: fc, datatype: dt, byteOrder, description });
  }

  return { tags, errors };
}
