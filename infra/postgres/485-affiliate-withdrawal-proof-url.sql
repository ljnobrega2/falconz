-- 485: proof_url no saque de afiliado (upload de comprovante igual ao produtor)
ALTER TABLE senderzz_affiliate_withdrawals ADD COLUMN IF NOT EXISTS proof_url TEXT NULL;
