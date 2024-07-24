function Image()
	return {}
end

function Header(el)
	-- Remove all Span elements from the header content
	el.content = el.content:filter(function(inline)
		return inline.t ~= "Span"
	end)
	return el
end
