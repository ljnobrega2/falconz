# Regularização do CD/galpão de expedição — São Paulo capital

**Cenário:** Centro de Distribuição que **armazena e expede** (Correios), **não aberto ao público**, **varejo B2C** (produto próprio, vendido ao consumidor final). Produtos: **cosmético** (notificado/registrado ANVISA) + **suplemento alimentar** (regime alimento, RDC 243/2018 + IN 28/2018).

> Documento de trabalho para contador/despachante. Datas e taxas confirmar no ato (valores municipais não fixados em fonte oficial). Pesquisa verificada em fontes oficiais SP capital + estado SP + federal (jun/2026).

---

## Veredito de escopo

| Item | Aplica? | Por quê |
|---|---|---|
| **AFE da ANVISA (cosmético)** | **NÃO — DISPENSADA** | Varejo ao consumidor final é isento (RDC 16/2014, art. 5º). Dispensa vale só enquanto **não importar direto** (importação reativa a AFE — RDC 860/2024). |
| **AFE da ANVISA (suplemento)** | **NÃO — INEXISTENTE** | ANVISA não emite AFE para alimentos. |
| **Licença de Funcionamento Sanitário (COVISA)** | **SIM — obrigatória** | CD que armazena+expede cosmético E suplemento. Cosmético força inspeção da UVIS. |
| **Zoneamento + ALF (Prefeitura)** | **SIM** | Estabelecimento físico. Galpão = uso nR armazenamento. |
| **CNPJ/IE no endereço real** | **SIM** | Fim da fachada vazia. |
| **Notificação do produto** | **SIM** | Suplemento notificado ANVISA; cosmético já notificado. |

**Caminho = 7 passos, SEM ANVISA federal.**

---

## Passo a passo (a ordem importa)

### 1. Zoneamento — checar ANTES de tudo
- **GeoSampa** (grátis, por SQL do IPTU): conferir a zona do endereço real.
- **Certidão de Uso e Ocupação do Solo** (SMUL/DEUSO, ~30 dias): documento formal.
- Galpão/CD = uso **não residencial "Serviços de Armazenamento e Guarda de Bens Móveis"** — nR1 ≤500m², nR2 500–5.000m², nR3 >5.000m² (Lei 16.402/2016 — LPUOS).
- ⚠️ **PROIBIDO em ZER** (Zona Exclusivamente Residencial, art. 17). Se o endereço for ZER → local inviável, resolver antes de gastar com o resto.
- Órgão: **Prefeitura SP — SMUL**
- Link: https://geosampa.prefeitura.sp.gov.br/PaginasPublicas/_SBC.aspx

### 2. CNPJ no endereço REAL (sair da fachada vazia)
- Alterar endereço via **REDESIM** (evento 211 — mesma cidade).
- CNAEs de **varejo**: cosmético **4772-5/00**; alimento/suplemento **4729-6** (confirmar subclasse com contador). **Não** usar 5211-7/99 (é só depósito de terceiros).
- Risco que isso elimina: **CNPJ inapto por "inexistência de fato"** (IN RFB 2.119/2022, art. 38) e **IE suspensa por não-localização** (visita fiscal SEFAZ-SP).
- Órgão: **Receita Federal / REDESIM** — gratuito.

### 3. Inscrição Estadual (IE / CADESP)
- Obrigatória por estabelecimento para emitir NF.
- Via **REDESIM**; se houver IE suspensa, restabelecer no SIPET.
- Órgão: **SEFAZ-SP — CADESP** — gratuito.

### 4. CCM + ALF (Auto de Licença de Funcionamento)
- Tirar **CCM** (Cadastro de Contribuinte Mobiliário).
- Emitir **ALF pelo sistema SLEA** (eletrônico): informa CCM, SQL (IPTU), atividade, área.
- **>150 m²** → exige responsável técnico (CREA/SP ou CAU/BR).
- Edificação irregular até 1.500 m² → **ALF-Condicionado** (validade 2 anos).
- Órgão: **Prefeitura SP — Fazenda (CCM) + Subprefeitura/SLEA (ALF)**
- Link: https://prefeitura.sp.gov.br/web/subprefeituras/w/sp_mais_facil/slea/330951

### 5. Licença de Funcionamento Sanitário (COVISA) — o coração da regularização
- Solicitar no **Portal Integrador VRE/REDESIM** → gera cadastro **CMVS** + número **CEVS**.
- Base: **Portaria SMS.G 266/2025** (documentos no Anexo I).
- Cobre **cosmético E suplemento** num único licenciamento.
- ⚠️ Cosmético **força inspeção da UVIS** no local (não sai só no CLI automático).
- **Galpão tem que estar adequado:**
  - Áreas separadas e identificadas: Recebimento, Quarentena, Aprovados, **Não Conformes**, Expedição.
  - **Segregar fisicamente alimento × cosmético**; nunca junto com saneante/produto químico.
  - Piso/paredes laváveis, controle integrado de pragas, paletes/estrados afastados de piso e parede.
  - Termohigrômetro calibrado, registro 2×/dia.
  - **Manual de Boas Práticas + POPs** (Portaria SMS 2.619/2011 — lado alimento): controle de lote/validade, **PVPS/FIFO**, rastreabilidade, área de vencidos/avariados.
  - Responsável com **curso de Boas Práticas (mín. 8h)** (lado alimento). RT habilitado depende do CNAE — confirmar na COVISA/UVIS.
  - AVCB (Bombeiros), desinsetização, PCMSO/PGR.
- Órgão: **COVISA / UVIS** (Prefeitura SP)
- Link: https://prefeitura.sp.gov.br/web/saude/w/vigilancia_em_saude/cmvs/como-solicitar

### 6. Regularidade do PRODUTO
- **Suplemento:** notificado na ANVISA (**RDC 843/2024** — prazo era 01/09/2025, deve já estar feito) + rótulo conforme RDC 243/2018 + IN 28/2018 + RDC 429/2020.
- **Cosmético:** notificação/registro que já existe.
- Recusar/segregar produto não conforme. Manter cópias das regularizações no local.

### 7. Religar NF + etiqueta ao endereço real
- NF-e emitida do galpão licenciado; **remetente Correios = endereço real** (fim da fachada vazia).
- **Restrição Correios:** proibido perfume/aerossol/cosmético **inflamável** (álcool/solvente). Líquido não perigoso: escrever "**Contém líquido não perigoso**" nas 3 faces + Declaração de Conteúdo/NF.
- Link: https://www.correios.com.br/enviar/proibicoes-e-restricoes

---

## Custos (confirmar no ato)
- **Zoneamento (GeoSampa), REDESIM, IE/CADESP:** gratuitos.
- **ALF + Licença Sanitária municipal:** taxas por tabela municipal (DARE/SIVISA) — **valor não fixado em fonte oficial da capital**; confirmar na COVISA/Fazenda-SP.
- **AFE / TFVS:** não se aplica (varejo).

## Gatilhos que MUDAM este roteiro (se ocorrerem)
- **Importar cosmético direto** → reativa AFE da ANVISA (RDC 860/2024).
- **Importar suplemento direto** → anuência ANVISA na LI (Siscomex) por embarque.
- **Passar a vender atacado/B2B** ou **guardar produto de terceiros (3PL)** → AFE de cosmético passa a ser exigida.
- **Endereço em ZER** → local inviável; trocar de imóvel.

## Pendências a confirmar no protocolo
1. Subclasse CNAE exata de varejo de alimentos/suplemento (com o contador).
2. Metragem do galpão (define nR1/2/3, RT no ALF >150m², teto do ALF-C 1.500m²).
3. RT habilitado exigido para o CNAE na COVISA, ou basta responsável com curso de BP 8h.
4. Valores vigentes das taxas municipais (ALF + licença sanitária).

---

### Fontes oficiais (SP capital / estado SP / federal)
- Licenciamento sanitário capital: https://prefeitura.sp.gov.br/web/saude/w/vigilancia_em_saude/cmvs/como-solicitar
- Portaria SMS.G 266/2025: https://legislacao.prefeitura.sp.gov.br/portaria-secretaria-municipal-da-saude-sms-266-de-6-de-maio-de-2025
- Boas Práticas alimento (Portaria SMS 2.619/2011): https://legislacao.prefeitura.sp.gov.br/leis/portaria-secretaria-municipal-da-saude-2619-de-6-de-dezembro-de-2011
- AFE — dispensa do varejo (FAQ ANVISA): https://www.gov.br/anvisa/pt-br/acessoainformacao/perguntasfrequentes/administrativo/autorizacao-de-funcionamento-afe-ou-ae/autorizacao-de-funcionamento-afe-ou-ae
- ANVISA não emite AFE para alimentos: https://www.gov.br/anvisa/pt-br/setorregulado/regularizacao/alimentos/regularizacao-de-empresas
- Zoneamento LPUOS (Lei 16.402/2016): https://legislacao.prefeitura.sp.gov.br/lei-16402-de-22-de-marco-de-2016
- ALF/SLEA: https://prefeitura.sp.gov.br/web/subprefeituras/w/sp_mais_facil/slea/330951
- IE não-localização (SEFAZ-SP/CADESP): https://portal.fazenda.sp.gov.br/servicos/cadesp/Paginas/Restabelecimento-de-IE-suspensa-por-n%C3%A3o-localiza%C3%A7%C3%A3o.aspx
- Correios — proibições/restrições: https://www.correios.com.br/enviar/proibicoes-e-restricoes
