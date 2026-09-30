# Finanz — finanças pessoais

MVP local para controle simples de receitas e despesas, sem login e sem backend.

## Rodar

```bash
npm install
npm run dev
```

O Vite exibirá o endereço local da aplicação. Os dados são salvos no `localStorage`
do navegador. Dados existentes da versão Clara são migrados automaticamente.

## Incluído no MVP

- perfis separados por pessoa, casa ou projeto;
- receitas e despesas realizadas ou previstas;
- categorias e formas de pagamento;
- saldo atual e previsão de fechamento mensal;
- previsão visual para os próximos seis meses;
- ranking de despesas por categoria;
- investimentos em produtos físicos e serviços;
- registro de vendas, faturamento, lucro bruto e margem;
- controle de unidades vendidas e estoque disponível para produtos físicos;
- busca, filtros e exclusão de lançamentos;
- layout responsivo para desktop e celular.

Não há sincronização entre dispositivos, controle de acesso ou backup remoto nesta
versão. Esses recursos exigem um backend e ficam fora do propósito do MVP local.
