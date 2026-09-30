import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const padronizadorUrl = new URL("../rotacionador/assets/padronizador-app.txt", import.meta.url);

function loadPadronizador({ xlsx = {} } = {}) {
  const html = readFileSync(padronizadorUrl, "utf8");
  const inlineScript = html.match(/<script>\s*([\s\S]*?)<\/script>/)?.[1];
  assert.ok(inlineScript, "inline padronizador script not found");

  const elements = new Map();
  const element = () => ({
    addEventListener() {},
    classList: { add() {}, remove() {}, toggle() {} },
    click() {},
    disabled: false,
    innerHTML: "",
    value: "zip",
    textContent: "",
  });
  const context = vm.createContext({
    Blob,
    TextDecoder,
    URL,
    location: { href: "https://example.com/assets/padronizador-app.html?mode=zip" },
    XLSX: xlsx,
    document: {
      body: { dataset: {} },
      createElement: element,
      querySelectorAll() { return []; },
      querySelector(selector) {
        if (!elements.has(selector)) elements.set(selector, element());
        return elements.get(selector);
      },
    },
    setTimeout,
  });

  vm.runInContext(inlineScript, context);
  return context;
}

function normalize(context, headers, rows) {
  context.__headers = headers;
  context.__rows = rows;
  return JSON.parse(vm.runInContext(
    "headers = __headers; JSON.stringify(normalizeRows(__rows))",
    context,
  ));
}

function packageDescriptors(context, headers, rows, extras) {
  context.__headers = headers;
  context.__rows = rows;
  context.__extras = extras;
  return JSON.parse(vm.runInContext(
    `
      headers = ensureControlColumns([...__headers]);
      mergedRows = addControls(normalizeRows(__rows));
      creativeFile = __extras.creativeFile || null;
      document.querySelector("#copy").value = __extras.copyText || "";
      document.querySelector("#button-link").value = __extras.buttonLink || "";
      document.querySelector("#output-name").value = __extras.outputName || "base-tratada";
      JSON.stringify(buildPackageDescriptors().map(({ path, kind, data }) => ({
        path,
        kind,
        data: typeof data === "string"
          ? data
          : (data && data.name) || (ArrayBuffer.isView(data) ? "binary" : "binary")
      })))
    `,
    context,
  ));
}

function todayStamp() {
  const date = new Date();
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

test("detecta e normaliza variantes comuns de nome, telefone e documento", () => {
  const context = loadPadronizador();
  const rows = normalize(
    context,
    ["Pessoa", "Cel.", "Cadastro"],
    [{ Pessoa: "  JOÃO   DA SILVA ", "Cel.": "(11) 98765-4321", Cadastro: "123.456.789-01" }],
  );

  assert.deepEqual(rows, [{
    Pessoa: "João da Silva",
    "Cel.": "5511987654321",
    Cadastro: "12345678901",
  }]);
});

test("separa telefones múltiplos sem concatená-los", () => {
  const context = loadPadronizador();
  const rows = normalize(
    context,
    ["Nome", "WhatsApp", "Origem"],
    [{ Nome: "ANA LIMA", WhatsApp: "(11) 98765-4321; (21) 99876-5432", Origem: "campanha-a" }],
  );

  assert.deepEqual(rows, [
    { Nome: "Ana Lima", WhatsApp: "5511987654321", Origem: "campanha-a" },
    { Nome: "Ana Lima", WhatsApp: "5521998765432", Origem: "campanha-a" },
  ]);
});

test("recusa sucesso enganoso quando nenhuma coluna ajustável é reconhecida", () => {
  const context = loadPadronizador();
  assert.throws(
    () => normalize(context, ["Código", "Origem"], [{ Código: "A1", Origem: "campanha-a" }]),
    /Nenhuma coluna de nome, telefone ou documento foi reconhecida/,
  );
});

test("não duplica um contato quando duas colunas contêm o mesmo telefone", () => {
  const context = loadPadronizador();
  const rows = normalize(
    context,
    ["phone_number", "formatted_phone", "saved_name"],
    [{
      phone_number: "5511987654321",
      formatted_phone: "+55 (11) 98765-4321",
      saved_name: "ANA LIMA",
    }],
  );

  assert.equal(rows.length, 1);
  assert.deepEqual(rows[0], {
    phone_number: "5511987654321",
    formatted_phone: "5511987654321",
    saved_name: "Ana Lima",
  });
});

test("ignora linhas de resumo que não possuem telefone", () => {
  const context = loadPadronizador();
  const rows = normalize(
    context,
    ["Telefone", "RESUMO DA UNIFICAÇÃO"],
    [
      { Telefone: "(11) 98765-4321", "RESUMO DA UNIFICAÇÃO": "" },
      { Telefone: "", "RESUMO DA UNIFICAÇÃO": "Total de contatos" },
    ],
  );

  assert.deepEqual(rows, [{ Telefone: "5511987654321", "RESUMO DA UNIFICAÇÃO": "" }]);
});

test("converte CSV de telefone exportado em notação científica com vírgula", async () => {
  const context = loadPadronizador({
    xlsx: {
      read() {
        return { SheetNames: ["Base"], Sheets: { Base: {} } };
      },
      utils: {
        sheet_to_json(_sheet, options) {
          if (options.header === 1) return [["Telefone"], ["5,59484E+11"], ["5,53789E+11"]];
          return [
            { Telefone: "5,59484E+11" },
            { Telefone: "5,53789E+11" },
          ];
        },
      },
    },
  });
  const bytes = new TextEncoder().encode("Telefone\r\n5,59484E+11\r\n5,53789E+11\r\n");
  context.__file = {
    name: "disparo.csv",
    arrayBuffer: async () => bytes.buffer,
  };

  const rawRows = await vm.runInContext("readFile(__file)", context);
  context.__headers = ["Telefone"];
  context.__rows = rawRows;

  const rows = JSON.parse(vm.runInContext("headers = __headers; JSON.stringify(normalizeRows(__rows))", context));

  assert.deepEqual(rows, [
    { Telefone: "559484000000" },
    { Telefone: "553789000000" },
  ]);
});

test("adiciona Telefone como cabeçalho de uma planilha de coluna única sem título", async () => {
  const matrix = [["11987654321"], ["21998765432"]];
  const context = loadPadronizador({
    xlsx: {
      read() {
        return { SheetNames: ["Base"], Sheets: { Base: {} } };
      },
      utils: {
        sheet_to_json(_sheet, options) {
          if (options.header === 1) return matrix;
          return [];
        },
      },
    },
  });
  const bytes = new TextEncoder().encode("11987654321\r\n21998765432\r\n");
  context.__file = { name: "sem-cabecalho.csv", arrayBuffer: async () => bytes.buffer };

  const rows = JSON.parse(JSON.stringify(await vm.runInContext("readFile(__file)", context)));

  assert.deepEqual(rows, [
    { Telefone: "11987654321", __source: "sem-cabecalho.csv", __sheet: "Base" },
    { Telefone: "21998765432", __source: "sem-cabecalho.csv", __sheet: "Base" },
  ]);
});

test("reduz planilha com fone_celular para uma única coluna Telefone", async () => {
  const matrix = [
    ["CD_CONTRATO", "NOME", "FONE_RES", "FONE_CELULAR", "TEL_RANK_1"],
    ["C-1", "ANA", "1133334444", "11987654321", "11911112222"],
    ["C-2", "BIA", "", "21998765432", "21922223333"],
  ];
  const context = loadPadronizador({
    xlsx: {
      read() {
        return { SheetNames: ["Planilha1"], Sheets: { Planilha1: {} } };
      },
      utils: {
        sheet_to_json(_sheet, options) {
          if (options.header === 1) return matrix;
          return [
            { CD_CONTRATO: "C-1", NOME: "ANA", FONE_RES: "1133334444", FONE_CELULAR: "11987654321", TEL_RANK_1: "11911112222" },
            { CD_CONTRATO: "C-2", NOME: "BIA", FONE_RES: "", FONE_CELULAR: "21998765432", TEL_RANK_1: "21922223333" },
          ];
        },
      },
    },
  });
  const bytes = new Uint8Array([1, 2, 3]);
  context.__file = { name: "sem-agenda.xlsx", arrayBuffer: async () => bytes.buffer };

  const rows = JSON.parse(JSON.stringify(await vm.runInContext("readFile(__file)", context)));

  assert.deepEqual(rows, [
    { Telefone: "11987654321", __source: "sem-agenda.xlsx", __sheet: "Planilha1" },
    { Telefone: "21998765432", __source: "sem-agenda.xlsx", __sheet: "Planilha1" },
  ]);
});

test("corrige 14 dígitos com 9 excedente e 12 dígitos sem 55", () => {
  const context = loadPadronizador();
  const rows = normalize(context, ["Telefone"], [
    { Telefone: "55229923551414" },
    { Telefone: "619827708191" },
  ]);

  assert.deepEqual(rows, [
    { Telefone: "5522923551414" },
    { Telefone: "5561827708191" },
  ]);
});

test("descarta telefones fora da regra sem cancelar a planilha", () => {
  const context = loadPadronizador();
  context.__rows = [
    { Telefone: "5511987654321" },
    { Telefone: "5511" },
    { Telefone: "54324523453245" },
  ];
  const result = JSON.parse(vm.runInContext(
    `
      headers = ["Telefone"];
      const rows = normalizeRows(__rows);
      JSON.stringify({ rows, stats: normalizationStats })
    `,
    context,
  ));

  assert.deepEqual(result.rows, [{ Telefone: "5511987654321" }]);
  assert.equal(result.stats.skipped, 2);
});

test("pré-visualiza somente as 30 primeiras linhas da planilha final", () => {
  const context = loadPadronizador();
  context.__rows = Array.from({ length: 40 }, (_, index) => ({ Telefone: `linha-${index + 1}` }));
  const markup = vm.runInContext('headers = ["Telefone"]; table(__rows)', context);

  assert.equal((markup.match(/<tbody><tr>|<tr>/g) || []).length, 31);
  assert.match(markup, /linha-30/);
  assert.doesNotMatch(markup, /linha-31/);
});

test("remove uma planilha selecionada sem apagar os outros campos", () => {
  const context = loadPadronizador();
  const result = JSON.parse(vm.runInContext(
    `
      selectedFiles = [
        { name: "base-antiga.csv", size: 100 },
        { name: "base-nova.xlsx", size: 200 }
      ];
      document.querySelector("#copy").value = "Copy preservada";
      document.querySelector("#button-link").value = "https://example.com/oferta";
      document.querySelector("#test-numbers").value = "11999990000";
      renderFiles();
      const markup = document.querySelector("#files-list").innerHTML;
      removeSelectedFile(0);
      JSON.stringify({
        markup,
        files: selectedFiles.map(file => file.name),
        copy: document.querySelector("#copy").value,
        link: document.querySelector("#button-link").value,
        tests: document.querySelector("#test-numbers").value
      })
    `,
    context,
  ));

  assert.match(result.markup, /data-remove-file="0"/);
  assert.deepEqual(result.files, ["base-nova.xlsx"]);
  assert.equal(result.copy, "Copy preservada");
  assert.equal(result.link, "https://example.com/oferta");
  assert.equal(result.tests, "11999990000");
});

test("prévia lateral representa planilha, criativo, link, contatos e copy do ZIP", () => {
  const context = loadPadronizador();
  const markup = vm.runInContext(
    `
      headers = ["Telefone"];
      mergedRows = [{ Telefone: "5511987654321" }];
      packageState = { rows: mergedRows };
      selectedFiles = [{ name: "leads.csv", size: 120 }];
      creativeFile = { name: "criativo.png", size: 300, type: "image/png" };
      document.querySelector("#copy").value = "Copy final da campanha";
      document.querySelector("#button-link").value = "https://example.com/comprar";
      document.querySelector("#test-numbers").value = "11999990000; 21988880000";
      document.querySelector("#output-name").value = "campanha-agosto";
      zipPreviewMarkup("blob:criativo")
    `,
    context,
  );

  assert.match(markup, /campanha-agosto-.*\.xlsx/);
  assert.match(markup, /5511987654321/);
  assert.match(markup, /criativo\.png/);
  assert.match(markup, /blob:criativo/);
  assert.match(markup, /https:\/\/example\.com\/comprar/);
  assert.match(markup, /5511999990000/);
  assert.match(markup, /5521988880000/);
  assert.match(markup, /Copy final da campanha/);
});

test("mantém o download do ZIP no cabeçalho da prévia lateral", () => {
  const html = readFileSync(padronizadorUrl, "utf8");
  const previewSection = html.match(/<section class="zip-only zip-preview-section">([\s\S]*?)<\/section>/)?.[1] || "";

  assert.match(previewSection, /zip-preview-title/);
  assert.match(previewSection, /id="download-zip"/);
  assert.equal((html.match(/id="download-zip"/g) || []).length, 1);
});

test("divide a base ao meio ou pela quantidade definida para o primeiro ZIP", () => {
  const context = loadPadronizador();
  context.__rows = Array.from({ length: 5 }, (_, index) => ({ Telefone: `contato-${index + 1}` }));

  const automatic = JSON.parse(vm.runInContext("JSON.stringify(splitBaseRows(__rows))", context));
  const custom = JSON.parse(vm.runInContext("JSON.stringify(splitBaseRows(__rows, '2'))", context));

  assert.deepEqual(automatic.map((part) => part.length), [3, 2]);
  assert.deepEqual(custom.map((part) => part.length), [2, 3]);
  assert.throws(
    () => vm.runInContext("splitBaseRows(__rows, '5')", context),
    /Informe de 1 a 4 contatos/,
  );
});

test("gera duas partes com controles e os mesmos materiais do ZIP", () => {
  const context = loadPadronizador({
    xlsx: {
      utils: {
        json_to_sheet() { return {}; },
        book_new() { return {}; },
        book_append_sheet() {},
      },
      write() { return new Uint8Array([1, 2, 3]); },
    },
  });
  const result = JSON.parse(vm.runInContext(
    `
      headers = ["Telefone"];
      const customers = [
        { Telefone: "5511987654301" },
        { Telefone: "5511987654302" },
        { Telefone: "5511987654303" },
        { Telefone: "5511987654304" },
        { Telefone: "5511987654305" }
      ];
      mergedRows = addControls(customers);
      packageState = { rows: mergedRows, customerRows: customers };
      document.querySelector("#split-base").checked = true;
      document.querySelector("#split-first-count").value = "2";
      document.querySelector("#copy").value = "Copy igual";
      document.querySelector("#button-link").value = "https://example.com/oferta";
      document.querySelector("#test-numbers").value = "11999990000";
      document.querySelector("#output-name").value = "campanha";
      creativeFile = { name: "criativo.png", size: 123, type: "image/png" };
      const parts = currentBaseParts();
      const packages = parts.map((part) => buildPackageDescriptors(part.rows, part.number, part.customers.length));
      renderSplitControls();
      JSON.stringify({
        counts: parts.map((part) => ({ customers: part.customers.length, final: part.rows.length })),
        bases: packages.map((items) => items[0].path),
        copies: packages.map((items) => items.find((item) => item.path === "copy/copy.txt").data),
        links: packages.map((items) => items.find((item) => item.path === "links/link-do-botao.txt").data),
        tests: packages.map((items) => items.find((item) => item.path === "testes/numeros-de-teste.txt").data),
        creatives: packages.map((items) => items.find((item) => item.kind === "file").path),
        summary: document.querySelector("#split-summary").textContent,
        button: document.querySelector("#download-zip").textContent
      })
    `,
    context,
  ));

  assert.deepEqual(result.counts, [
    { customers: 2, final: 7 },
    { customers: 3, final: 8 },
  ]);
  assert.match(result.bases[0], /campanha-.*-parte-1\.xlsx$/);
  assert.match(result.bases[1], /campanha-.*-parte-2\.xlsx$/);
  assert.deepEqual(result.copies, ["Copy igual", "Copy igual"]);
  assert.deepEqual(result.links, ["https://example.com/oferta", "https://example.com/oferta"]);
  assert.deepEqual(result.tests, ["5511999990000\n", "5511999990000\n"]);
  assert.deepEqual(result.creatives, ["criativo/criativo.png", "criativo/criativo.png"]);
  assert.match(result.summary, /ZIP 1: 2 contato.*ZIP 2: 3 contato/);
  assert.equal(result.button, "Baixar 2 ZIPs");
});

test("descarta contatos repetidos em linhas distintas", () => {
  const context = loadPadronizador();
  const rows = normalize(
    context,
    ["Nome", "Telefone"],
    [
      { Nome: "ANA LIMA", Telefone: "(11) 98765-4321" },
      { Nome: "ANA LIMA", Telefone: "(11) 98765-4321" },
    ],
  );

  assert.equal(rows.length, 1);
});

test("monta o pacote final com base, copy, link e criativo", () => {
  const context = loadPadronizador({
    xlsx: {
      utils: {
        json_to_sheet() { return {}; },
        book_new() { return {}; },
        book_append_sheet() {},
      },
      write() { return new Uint8Array([1, 2, 3]); },
    },
  });

  const entries = packageDescriptors(
    context,
    ["Nome", "Telefone"],
    [{ Nome: "ANA LIMA", Telefone: "(11) 98765-4321" }],
    {
      copyText: "Teste de copy",
      buttonLink: "https://example.com/oferta",
      outputName: "base-teste",
      creativeFile: { name: "criativo.mp4", size: 1234 },
    },
  );

  assert.deepEqual(entries.map((entry) => [entry.path, entry.kind]), [
    [`base/base-teste-${todayStamp()}.xlsx`, "array"],
    ["copy/copy.txt", "text"],
    ["links/link-do-botao.txt", "text"],
    ["manifesto.txt", "text"],
    ["criativo/criativo.mp4", "file"],
  ]);
  assert.match(entries.find((entry) => entry.path === "manifesto.txt").data, /Teste de copy/);
  assert.match(entries.find((entry) => entry.path === "manifesto.txt").data, /https:\/\/example\.com\/oferta/);
});

test("mantém só a planilha quando a saída é xlsx", () => {
  const context = loadPadronizador({
    xlsx: {
      utils: {
        json_to_sheet() { return {}; },
        book_new() { return {}; },
        book_append_sheet() {},
      },
      write() { return new Uint8Array([1, 2, 3]); },
    },
  });

  const entries = packageDescriptors(
    context,
    ["Nome", "Telefone"],
    [{ Nome: "ANA LIMA", Telefone: "(11) 98765-4321" }],
    {
      copyText: "Teste de copy",
      buttonLink: "https://example.com/oferta",
      outputName: "base-teste",
    },
  );

  assert.equal(entries.find((entry) => entry.path === `base/base-teste-${todayStamp()}.xlsx`).kind, "array");
  assert.equal(entries.some((entry) => entry.path === "criativo/sem-criativo.txt"), true);
});

test("mantém os números de controle na coluna de telefone existente", () => {
  const context = loadPadronizador({
    xlsx: {
      utils: {
        json_to_sheet() { return {}; },
        book_new() { return {}; },
        book_append_sheet() {},
      },
      write() { return new Uint8Array([1, 2, 3]); },
    },
  });

  const result = JSON.parse(vm.runInContext(
    `
      headers = ensureControlColumns(["Nome", "Telefone"]);
      JSON.stringify({
        headers,
        rows: addControls(normalizeRows([{ Nome: "ANA LIMA", Telefone: "(11) 98765-4321" }]))
          .filter(row => CONTROLS.some(control => row.Telefone === control.phone))
      })
    `,
    context,
  ));

  assert.deepEqual(result.headers, ["Nome", "Telefone"]);
  assert.equal(result.rows.length, 5);
  assert.deepEqual(result.rows.map((row) => Object.keys(row)), [
    ["Nome", "Telefone"],
    ["Nome", "Telefone"],
    ["Nome", "Telefone"],
    ["Nome", "Telefone"],
    ["Nome", "Telefone"],
  ]);
  assert.deepEqual(result.rows.map((row) => row.Telefone), [
    "5511963486603",
    "5512981965514",
    "5511986045939",
    "5561998646652",
    "5521990706929",
  ]);
});

test("inclui números de teste na coluna de telefone existente", () => {
  const context = loadPadronizador();
  const result = JSON.parse(vm.runInContext(
    `
      headers = ensureControlColumns(["Nome", "Telefone"]);
      document.querySelector("#test-numbers").value = "(11) 99999-0000; 21988880000";
      JSON.stringify(addControls(normalizeRows([{ Nome: "ANA LIMA", Telefone: "(11) 98765-4321" }]), true)
        .filter(row => row.Telefone === "5511999990000" || row.Telefone === "5521988880000"))
    `,
    context,
  ));

  assert.deepEqual(result, [
    { Nome: "", Telefone: "5511999990000" },
    { Nome: "", Telefone: "5521988880000" },
  ]);
});
