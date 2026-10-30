import { describe, expect, it } from 'vitest';
import { parseTemplate, render, TemplateError } from '@/lib/recipe/templates';

const ctx = { shopeeShopId: '123456', text: { caption: 'Summer sale', empty: '' } };
const req = (n: string) => n !== 'optional';

describe('templates', () => {
  it('renders the shop id and text inputs', () => {
    expect(render('Shop {{task.shopee_shop_id}}: {{input.caption}}', ctx, req)).toBe('Shop 123456: Summer sale');
    expect(render('{{ input.caption }}', ctx, req)).toBe('Summer sale');
    expect(render('no templates', ctx, req)).toBe('no templates');
  });

  it('does not re-expand template syntax inside values', () => {
    const c = { shopeeShopId: '1', text: { caption: '{{task.shopee_shop_id}}' } };
    expect(render('{{input.caption}}', c, req)).toBe('{{task.shopee_shop_id}}');
  });

  it('renders a missing optional input as empty and throws for a missing required one', () => {
    expect(render('[{{input.optional}}]', ctx, req)).toBe('[]');
    expect(() => render('{{input.required_one}}', ctx, req)).toThrow(TemplateError);
  });

  it('rejects anything but the two allowed forms', () => {
    expect(() => render('{{task.org_id}}', ctx, req)).toThrow(TemplateError);
    expect(parseTemplate('{{input.Caption}}').issues).toHaveLength(1);
    expect(parseTemplate('{{input.a.b}}').issues).toHaveLength(1);
    expect(parseTemplate('{{ }}').issues).toHaveLength(1);
    expect(parseTemplate('a {{ b').issues).toHaveLength(1);
    expect(parseTemplate('{{input.caption}} {{task.shopee_shop_id}}').refs).toEqual([
      { kind: 'input', name: 'caption' },
      { kind: 'task_shop_id' },
    ]);
  });
});
