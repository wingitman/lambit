/**
 * node-transform — lambit test lambda (Node.js ESM)
 *
 * Exports three lambda handlers that exercise all lambit features:
 *   - index.transform    text transformations (upper/lower/reverse/slugify/wordcount)
 *   - index.calculate    maths operations (add/subtract/multiply/divide/power/sqrt)
 *   - index.summarise    summarise an array of numbers (min/max/mean/median/mode)
 *
 * Handler strings (for .lambit.toml):
 *   index.transform
 *   index.calculate
 *   index.summarise
 */

// ─── transform ────────────────────────────────────────────────────────────────

/**
 * Transform a string using one of several operations.
 * @param {{ text: string, operation: string }} event
 */
export async function transform(event, _context) {
  const { text = '', operation = 'upper' } = event;

  if (!text) throw new Error('text must not be empty');

  let result;
  switch (operation.toLowerCase()) {
    case 'upper':
      result = text.toUpperCase();
      break;
    case 'lower':
      result = text.toLowerCase();
      break;
    case 'reverse':
      result = text.split('').reverse().join('');
      break;
    case 'slugify':
      result = text
        .toLowerCase()
        .trim()
        .replace(/[^\w\s-]/g, '')
        .replace(/[\s_-]+/g, '-')
        .replace(/^-+|-+$/g, '');
      break;
    case 'wordcount': {
      const count = text.trim().split(/\s+/).filter(Boolean).length;
      return { original: text, operation, wordCount: count };
    }
    case 'titlecase':
      result = text.replace(/\b\w/g, c => c.toUpperCase());
      break;
    default:
      throw new Error(`Unknown operation: ${operation}. Valid: upper, lower, reverse, slugify, wordcount, titlecase`);
  }

  return { original: text, transformed: result, operation };
}

// ─── calculate ────────────────────────────────────────────────────────────────

/**
 * Perform a maths operation on two numbers (or one for unary ops).
 * @param {{ a: number, b?: number, operation: string }} event
 */
export async function calculate(event, _context) {
  const { a, b, operation = 'add' } = event;

  if (typeof a !== 'number') throw new Error('"a" must be a number');

  let result;
  switch (operation.toLowerCase()) {
    case 'add':      result = a + (b ?? 0);  break;
    case 'subtract': result = a - (b ?? 0);  break;
    case 'multiply': result = a * (b ?? 1);  break;
    case 'divide':
      if (b === 0) throw new Error('Division by zero');
      result = a / (b ?? 1);
      break;
    case 'power':    result = Math.pow(a, b ?? 2);  break;
    case 'sqrt':
      if (a < 0) throw new Error('Cannot take sqrt of a negative number');
      result = Math.sqrt(a);
      break;
    case 'abs':      result = Math.abs(a);   break;
    case 'mod':
      if (b === 0) throw new Error('Modulo by zero');
      result = a % (b ?? 1);
      break;
    default:
      throw new Error(`Unknown operation: ${operation}`);
  }

  return { a, b, operation, result };
}

// ─── summarise ────────────────────────────────────────────────────────────────

/**
 * Compute statistical summary of an array of numbers.
 * @param {{ numbers: number[] }} event
 */
export async function summarise(event, _context) {
  const { numbers = [] } = event;

  if (!Array.isArray(numbers) || numbers.length === 0)
    throw new Error('"numbers" must be a non-empty array');

  if (numbers.some(n => typeof n !== 'number'))
    throw new Error('All elements of "numbers" must be numbers');

  const sorted = [...numbers].sort((a, b) => a - b);
  const sum    = numbers.reduce((acc, n) => acc + n, 0);
  const mean   = sum / numbers.length;

  const mid    = Math.floor(sorted.length / 2);
  const median = sorted.length % 2 === 0
    ? (sorted[mid - 1] + sorted[mid]) / 2
    : sorted[mid];

  // Mode — the value(s) appearing most frequently.
  const freq = new Map();
  for (const n of numbers) freq.set(n, (freq.get(n) ?? 0) + 1);
  const maxFreq = Math.max(...freq.values());
  const mode    = [...freq.entries()]
    .filter(([, f]) => f === maxFreq)
    .map(([v]) => v);

  return {
    count:  numbers.length,
    sum,
    min:    sorted[0],
    max:    sorted[sorted.length - 1],
    mean:   Math.round(mean * 1000) / 1000,
    median,
    mode:   mode.length === numbers.length ? null : mode, // null = all unique
    range:  sorted[sorted.length - 1] - sorted[0],
  };
}
