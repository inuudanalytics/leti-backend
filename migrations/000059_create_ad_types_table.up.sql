
CREATE TYPE ad_duration_type AS ENUM (
    'daily',
    'weekly',
    'biweekly',
    'monthly'
);
 
CREATE TYPE ad_target_type AS ENUM (
    'artisan',
    'owner'
);
 
CREATE TYPE ad_payment_method AS ENUM (
    'wallet',
    'paystack'
);
 
CREATE TYPE ad_campaign_status AS ENUM (
    'pending',       
    'active',         
    'paused',         
    'auto_paused',   
    'completed',      
    'cancelled'       
);
 
CREATE TYPE ad_campaign_mode AS ENUM (
    'one_time',
    'recurring'
);