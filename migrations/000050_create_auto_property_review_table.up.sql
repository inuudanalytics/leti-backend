CREATE OR REPLACE FUNCTION update_property_rating()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE properties
    SET
        avg_rating   = (
            SELECT COALESCE(ROUND(AVG(rating)::NUMERIC, 2), 0.00)
            FROM property_reviews
            WHERE property_id = COALESCE(NEW.property_id, OLD.property_id)
        ),
        review_count = (
            SELECT COUNT(*)
            FROM property_reviews
            WHERE property_id = COALESCE(NEW.property_id, OLD.property_id)
        )
    WHERE id = COALESCE(NEW.property_id, OLD.property_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
 
CREATE TRIGGER trg_update_property_rating
    AFTER INSERT OR UPDATE OR DELETE ON property_reviews
    FOR EACH ROW EXECUTE FUNCTION update_property_rating();